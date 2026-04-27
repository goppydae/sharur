// Package service implements the core gollm logic behind a Protobuf-defined interface.
package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/config"
	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// Service implements pb.AgentServiceServer.
// It manages agent instances, sessions, and translations between internal events and Protobuf messages.
type Service struct {
	pb.UnimplementedAgentServiceServer

	provider   llm.Provider
	registry   *tools.ToolRegistry
	extensions []agent.Extension
	rootCtx    context.Context
	manager    *session.Manager
	cfg        *config.Config

	mu       sync.RWMutex
	sessions map[string]*sessionEntry
}

type sessionEntry struct {
	ag       *agent.Agent
	cancelHB context.CancelFunc
	lastUsed time.Time
}

// New creates a new Service.
func New(ctx context.Context, provider llm.Provider, registry *tools.ToolRegistry, mgr *session.Manager, exts []agent.Extension) *Service {
	s := &Service{
		provider:   provider,
		registry:   registry,
		extensions: exts,
		rootCtx:    ctx,
		manager:    mgr,
		sessions:   make(map[string]*sessionEntry),
	}
	go s.runEviction(ctx)
	return s
}

// runEviction periodically removes idle sessions from memory. Sessions that are
// still running or have been used recently are preserved; they can always be
// reloaded from disk on next access.
func (s *Service) runEviction(ctx context.Context) {
	t := time.NewTicker(evictionCheckPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.evictIdleSessions()
		}
	}
}

func (s *Service) evictIdleSessions() {
	cutoff := time.Now().Add(-sessionIdleTTL)
	s.mu.Lock()
	for id, e := range s.sessions {
		if e.ag.IsRunning() || e.lastUsed.After(cutoff) {
			continue
		}
		e.cancelHB()
		delete(s.sessions, id)
	}
	s.mu.Unlock()
}

// WithConfig attaches configuration used for provider rebuilding (e.g. /model provider/model).
func (s *Service) WithConfig(cfg *config.Config) *Service {
	s.cfg = cfg
	return s
}

// getOrCreate returns the sessionEntry for id, creating a new agent if needed.
func (s *Service) getOrCreate(id string) *sessionEntry {
	if id == "" {
		id = uuid.New().String()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.sessions[id]; ok {
		e.lastUsed = time.Now()
		return e
	}
	ag := agent.New(s.provider, s.registry)
	ag.SetExtensions(s.extensions)

	if s.manager != nil {
		saved, err := s.manager.Load(id)
		if err != nil {
			// Create new session with this ID
			saved, err = s.manager.CreateWithID(id)
		}
		if err == nil {
			ag.SetSession(s.manager, saved)
		}
	}

	hbCtx, hbCancel := context.WithCancel(s.rootCtx)
	e := &sessionEntry{ag: ag, cancelHB: hbCancel, lastUsed: time.Now()}
	s.sessions[id] = e
	go s.runHeartbeat(hbCtx, ag, id)
	return e
}

func (s *Service) lookup(id string) (*sessionEntry, bool) {
	s.mu.RLock()
	e, ok := s.sessions[id]
	s.mu.RUnlock()
	return e, ok
}

// loadIfExists returns the sessionEntry for id, loading it from disk if it
// exists there but is not yet in memory. Returns (nil, false) for IDs that
// have no on-disk session file, so callers can return NotFound.
func (s *Service) loadIfExists(id string) (*sessionEntry, bool) {
	if e, ok := s.lookup(id); ok {
		s.mu.Lock()
		e.lastUsed = time.Now()
		s.mu.Unlock()
		return e, true
	}
	if s.manager == nil {
		return nil, false
	}

	// Try to resolve the ID (might be a session ID or a record ID)
	saved, err := s.manager.Load(id)
	if err != nil {
		saved, err = s.manager.LoadForRecord(id)
		if err != nil {
			return nil, false
		}
	}

	// Re-check memory using the canonical session ID
	if e, ok := s.lookup(saved.ID); ok {
		s.mu.Lock()
		e.lastUsed = time.Now()
		s.mu.Unlock()
		return e, true
	}

	s.mu.Lock()
	// Double-check after acquiring write lock.
	if e, ok := s.sessions[saved.ID]; ok {
		e.lastUsed = time.Now()
		s.mu.Unlock()
		return e, true
	}
	ag := agent.New(s.provider, s.registry)
	ag.SetExtensions(s.extensions)
	ag.SetSession(s.manager, saved)
	hbCtx, hbCancel := context.WithCancel(s.rootCtx)
	e := &sessionEntry{ag: ag, cancelHB: hbCancel, lastUsed: time.Now()}
	s.sessions[saved.ID] = e
	s.mu.Unlock()
	go s.runHeartbeat(hbCtx, ag, saved.ID)
	return e, true
}

const (
	heartbeatInterval  = 5 * time.Second
	sessionIdleTTL     = 30 * time.Minute
	evictionCheckPeriod = 5 * time.Minute
)

func (s *Service) runHeartbeat(ctx context.Context, ag *agent.Agent, sessionID string) {
	t := time.NewTicker(heartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if ag.IsRunning() {
				ag.EventBus().Publish(agent.Event{
					Type:    agent.EventHeartbeat,
					Value:   time.Now().UnixMilli(),
					Content: ag.LifecycleState(),
				})
			}
		}
	}
}

func (s *Service) saveSession(id string) {
	// Most saving is now handled in-turn by Agent calling mgr.Append*.
}

func agentStateToSession(st *agent.AgentState) *session.Session {
	return &session.Session{
		ID:            st.Session.ID,
		Name:          st.Session.Name,
		CreatedAt:     st.Session.CreatedAt,
		UpdatedAt:     time.Now(),
		Model:         st.Model,
		Provider:      st.Provider,
		Thinking:      string(st.Thinking),
		SystemPrompt:  st.SystemPrompt,
		Messages:      st.Messages,
		DryRun:        st.DryRun,
		CompactionEnabled:   st.Compaction.Enabled,
		CompactionReserve:   st.Compaction.ReserveTokens,
		CompactionKeep:      st.Compaction.KeepRecentTokens,
		ParentID:            st.Session.ParentID,
		ParentMessageIndex:  st.Session.ParentMessageIndex,
		MergeSourceID:       st.Session.MergeSourceID,
	}
}

// SaveAllSessions flushes all in-memory sessions to disk.
func (s *Service) SaveAllSessions() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		s.saveSession(id)
	}
}

// StopAllSessions aborts any in-flight turns.
func (s *Service) StopAllSessions() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.sessions {
		if e.ag.IsRunning() {
			e.ag.Abort()
		}
	}
}

// ── RPC Implementations ────────────────────────────────────────────────────────

func (s *Service) Prompt(req *pb.PromptRequest, stream pb.AgentService_PromptServer) error {
	if req.SessionId == "" {
		return status.Error(codes.InvalidArgument, "session_id required")
	}
	e := s.getOrCreate(req.SessionId)
	ag := e.ag

	streamDone := make(chan struct{})
	var streamDoneOnce sync.Once
	closeStreamDone := func() { streamDoneOnce.Do(func() { close(streamDone) }) }

	sendErr := make(chan error, 1)

	unsub := ag.Subscribe(func(ev agent.Event) {
		msg := eventToProto(req.SessionId, ev)
		if msg != nil {
			if err := stream.Send(msg); err != nil {
				select {
				case sendErr <- err:
				default:
				}
				closeStreamDone()
				return
			}
		}
		if isTerminalEvent(ev.Type) {
			closeStreamDone()
		}
	})
	defer unsub()

	var imgs []agent.Image
	for _, img := range req.Images {
		imgs = append(imgs, agent.Image{MIMEType: img.MimeType, Data: img.Data})
	}

	if err := ag.Prompt(stream.Context(), req.Message, imgs...); err != nil {
		return status.Errorf(codes.FailedPrecondition, "prompt: %v", err)
	}

	select {
	case <-streamDone:
		s.saveSession(req.SessionId)
		return nil
	case err := <-sendErr:
		return err
	case <-stream.Context().Done():
		return nil
	}
}

func (s *Service) Steer(_ context.Context, req *pb.SteerRequest) (*pb.SteerResponse, error) {
	e, ok := s.lookup(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	e.ag.Steer(req.Message)
	return &pb.SteerResponse{Ok: true}, nil
}

func (s *Service) Abort(_ context.Context, req *pb.AbortRequest) (*pb.AbortResponse, error) {
	e, ok := s.lookup(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	e.ag.Abort()
	return &pb.AbortResponse{Ok: true}, nil
}

func (s *Service) FollowUp(_ context.Context, req *pb.FollowUpRequest) (*pb.FollowUpResponse, error) {
	e, ok := s.lookup(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	
	// Convert pb images to agent images
	var imgs []agent.Image
	for _, img := range req.Images {
		imgs = append(imgs, agent.Image{
			MIMEType: img.MimeType,
			Data:     img.Data,
		})
	}

	e.ag.FollowUp(req.Message, imgs...)
	return &pb.FollowUpResponse{Ok: true}, nil
}

func (s *Service) StreamEvents(req *pb.StreamEventsRequest, stream pb.AgentService_StreamEventsServer) error {
	e, ok := s.lookup(req.SessionId)
	if !ok {
		return status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}

	sendErr := make(chan error, 1)

	unsub := e.ag.Subscribe(func(ev agent.Event) {
		p := eventToProto(req.SessionId, ev)
		if p != nil {
			if err := stream.Send(p); err != nil {
				select {
				case sendErr <- err:
				default:
				}
			}
		}
	})
	defer unsub()

	select {
	case <-stream.Context().Done():
		return nil
	case err := <-sendErr:
		return err
	}
}

func (s *Service) NewSession(_ context.Context, req *pb.NewSessionRequest) (*pb.NewSessionResponse, error) {
	id := req.SessionId
	if id == "" {
		id = uuid.New().String()
	}
	s.mu.Lock()
	if e, ok := s.sessions[id]; ok {
		if e.ag.IsRunning() {
			s.mu.Unlock()
			return nil, status.Error(codes.FailedPrecondition, "agent is running")
		}
		e.ag.Reset()
		if req.Name != "" {
			e.ag.SetSessionName(req.Name)
		}
		s.mu.Unlock()
		return &pb.NewSessionResponse{SessionId: id}, nil
	}
	s.mu.Unlock()
	s.getOrCreate(id)
	return &pb.NewSessionResponse{SessionId: id}, nil
}

func (s *Service) DeleteSession(_ context.Context, req *pb.DeleteSessionRequest) (*pb.DeleteSessionResponse, error) {
	s.mu.Lock()
	e, ok := s.sessions[req.SessionId]
	if ok {
		e.cancelHB()
		delete(s.sessions, req.SessionId)
	}
	s.mu.Unlock()
	if ok && s.manager != nil {
		_ = s.manager.Delete(req.SessionId)
	}
	return &pb.DeleteSessionResponse{Ok: ok}, nil
}

func (s *Service) ListSessions(_ context.Context, _ *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	s.mu.RLock()
	summaries := make([]*pb.SessionSummary, 0, len(s.sessions))
	inMemory := make(map[string]struct{}, len(s.sessions))
	for id, e := range s.sessions {
		inMemory[id] = struct{}{}
		st := e.ag.State()
		summaries = append(summaries, &pb.SessionSummary{
			SessionId:   id,
			Name:        st.Session.Name,
			Description: "", // We could extract first message here if needed
			Lifecycle:   e.ag.LifecycleState(),
			CreatedAt:   st.Session.CreatedAt.Unix(),
			UpdatedAt:   st.Session.UpdatedAt.Unix(),
		})
	}
	s.mu.RUnlock()

	if s.manager != nil {
		if diskSummaries, err := s.manager.ListSummaries(); err == nil {
			for _, ds := range diskSummaries {
				if _, loaded := inMemory[ds.ID]; loaded {
					continue
				}
				summaries = append(summaries, &pb.SessionSummary{
					SessionId:   ds.ID,
					Name:        ds.Name,
					Description: ds.FirstMessage,
					Lifecycle:   "idle",
					CreatedAt:   ds.CreatedAt.Unix(),
					UpdatedAt:   ds.UpdatedAt.Unix(),
				})
			}
		}
	}
	return &pb.ListSessionsResponse{Sessions: summaries}, nil
}

func (s *Service) GetState(_ context.Context, req *pb.GetStateRequest) (*pb.GetStateResponse, error) {
	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	st := e.ag.State()
	info := e.ag.GetInfo()

	resp := &pb.GetStateResponse{
		SessionId:     req.SessionId,
		Model:         st.Model,
		Provider:      st.Provider,
		ThinkingLevel: string(st.Thinking),
		Lifecycle:     e.ag.LifecycleState(),
		MessageCount:  int32(len(st.Messages)),
		SystemPrompt:  st.SystemPrompt,
		DryRun:        st.DryRun,
		Compaction: &pb.CompactionConfig{
			Enabled:          st.Compaction.Enabled,
			ReserveTokens:    int32(st.Compaction.ReserveTokens),
			KeepRecentTokens: int32(st.Compaction.KeepRecentTokens),
		},
		ProviderInfo: &pb.ProviderInfo{
			Name:           info.Name,
			Model:          info.Model,
			ContextWindow:  int32(info.ContextWindow),
			SupportsImages: info.HasImages,
			SupportsTools:  info.HasToolCall,
		},
		ParentMessageIndex: -1,
	}
	if st.Session.ParentMessageIndex != nil {
		resp.ParentMessageIndex = int32(*st.Session.ParentMessageIndex)
	}
	if st.Session.MergeSourceID != nil {
		resp.MergeSourceId = *st.Session.MergeSourceID
	}
	// RebasedFrom is on the disk session; load it from manager if available.
	if s.manager != nil {
		if saved, err := s.manager.Load(req.SessionId); err == nil && saved.RebasedFrom != nil {
			resp.RebasedFrom = *saved.RebasedFrom
		}
	}
	return resp, nil
}

func (s *Service) GetMessages(_ context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error) {
	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	msgs := e.ag.Messages()
	out := make([]*pb.ConversationMessage, 0, len(msgs))
	for _, m := range msgs {
		cm := &pb.ConversationMessage{
			Role:       m.Role,
			Content:    m.Content,
			Timestamp:  m.Timestamp.UnixMilli(),
			Thinking:   m.Thinking,
			ToolCallId: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, &pb.ToolCallProto{
				Id:      tc.ID,
				Name:    tc.Name,
				ArgsJson: string(tc.Args),
			})
		}
		out = append(out, cm)
	}
	return &pb.GetMessagesResponse{Messages: out}, nil
}

func (s *Service) ConfigureSession(_ context.Context, req *pb.ConfigureSessionRequest) (*pb.ConfigureSessionResponse, error) {
	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}

	if req.Model != nil {
		e.ag.SetModel(*req.Model)
	}
	if req.ThinkingLevel != nil {
		e.ag.SetThinkingLevel(types.ThinkingLevel(*req.ThinkingLevel))
	}
	if req.SystemPrompt != nil {
		e.ag.SetSystemPrompt(*req.SystemPrompt)
	}
	if req.DryRun != nil {
		e.ag.SetDryRun(*req.DryRun)
	}
	if req.Compaction != nil {
		e.ag.SetCompactionConfig(req.Compaction.Enabled, int(req.Compaction.ReserveTokens), int(req.Compaction.KeepRecentTokens))
	}
	if req.Provider != nil && s.cfg != nil {
		provCfg := *s.cfg
		provCfg.Provider = *req.Provider
		if req.Model != nil {
			provCfg.Model = *req.Model
		}
		if prov, err := config.BuildProvider(&provCfg); err == nil {
			e.ag.SetProvider(prov)
		}
	}

	return &pb.ConfigureSessionResponse{Ok: true}, nil
}

func (s *Service) SetModel(_ context.Context, req *pb.SetModelRequest) (*pb.SetModelResponse, error) {
	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	e.ag.SetModel(req.Model)
	return &pb.SetModelResponse{Ok: true}, nil
}

func (s *Service) SetThinkingLevel(_ context.Context, req *pb.SetThinkingLevelRequest) (*pb.SetThinkingLevelResponse, error) {
	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	e.ag.SetThinkingLevel(types.ThinkingLevel(req.ThinkingLevel))
	return &pb.SetThinkingLevelResponse{Ok: true}, nil
}

func (s *Service) SetSessionName(_ context.Context, req *pb.SetSessionNameRequest) (*pb.SetSessionNameResponse, error) {
	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	e.ag.SetSessionName(req.Name)
	return &pb.SetSessionNameResponse{Ok: true}, nil
}

func (s *Service) Compact(ctx context.Context, req *pb.CompactRequest) (*pb.CompactResponse, error) {
	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}
	e.ag.Compact(ctx, 0)
	return &pb.CompactResponse{Ok: true}, nil
}

func (s *Service) BranchSession(_ context.Context, req *pb.BranchSessionRequest) (*pb.NewSessionResponse, error) {
	if s.manager == nil {
		return nil, status.Error(codes.Unimplemented, "session persistence disabled")
	}

	id := req.SessionId
	if req.TargetId != "" {
		id = req.TargetId
	}

	e, ok := s.loadIfExists(id)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session or record %q not found", id)
	}

	source := e.ag.Session()
	if source == nil {
		return nil, status.Error(codes.FailedPrecondition, "session not initialized")
	}

	leafID := source.CurrentLeafID
	if req.TargetId != "" {
		leafID = req.TargetId
	}

	branched, err := s.manager.BranchAt(source, leafID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "branch session: %v", err)
	}

	s.getOrCreate(branched.ID)
	return &pb.NewSessionResponse{SessionId: branched.ID}, nil
}

func (s *Service) ForkSession(_ context.Context, req *pb.ForkSessionRequest) (*pb.NewSessionResponse, error) {
	if s.manager == nil {
		return nil, status.Error(codes.Unimplemented, "session persistence disabled")
	}

	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session or record %q not found", req.SessionId)
	}

	source := e.ag.Session()
	if source == nil {
		return nil, status.Error(codes.FailedPrecondition, "session not initialized")
	}

	// Fork creates an independent copy at the current leaf
	forked, err := s.manager.Fork(source)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fork session: %v", err)
	}

	s.getOrCreate(forked.ID)
	return &pb.NewSessionResponse{SessionId: forked.ID}, nil
}

func (s *Service) RebaseSession(ctx context.Context, req *pb.RebaseSessionRequest) (*pb.NewSessionResponse, error) {
	if s.manager == nil {
		return nil, status.Error(codes.Unimplemented, "session persistence disabled")
	}
	e, ok := s.loadIfExists(req.SessionId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "session %q not found", req.SessionId)
	}

	st := e.ag.State()
	source := agentStateToSession(st)

	recordIDs := make([]string, 0, len(req.MsgIndices))
	for _, idx := range req.MsgIndices {
		if int(idx) >= 0 && int(idx) < len(st.Messages) {
			recordIDs = append(recordIDs, st.Messages[idx].ID)
		}
	}

	// TODO: implement squash logic if req.Squash is true

	rebased, err := s.manager.Rebase(source, recordIDs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rebase session: %v", err)
	}

	s.getOrCreate(rebased.ID)
	return &pb.NewSessionResponse{SessionId: rebased.ID}, nil
}

func (s *Service) MergeSession(ctx context.Context, req *pb.MergeSessionRequest) (*pb.NewSessionResponse, error) {
	if s.manager == nil {
		return nil, status.Error(codes.Unimplemented, "session persistence disabled")
	}

	loadSource := func(id string) (*session.Session, error) {
		if e, ok := s.loadIfExists(id); ok {
			return agentStateToSession(e.ag.State()), nil
		}
		return nil, status.Errorf(codes.NotFound, "session %q not found", id)
	}

	sessA, err := loadSource(req.SessionIdA)
	if err != nil {
		return nil, err
	}
	sessB, err := loadSource(req.SessionIdB)
	if err != nil {
		return nil, err
	}

	synthesisText, err := s.buildMergeSynthesis(ctx, sessA, sessB)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "merge synthesis: %v", err)
	}

	now := time.Now()
	mergedID := uuid.New().String()
	merged := &session.Session{
		ID:            mergedID,
		ParentID:      &sessA.ID,
		MergeSourceID: &sessB.ID,
		Model:         sessA.Model,
		Provider:      sessA.Provider,
		Thinking:      sessA.Thinking,
		SystemPrompt:  sessA.SystemPrompt,
		CreatedAt:     now,
		UpdatedAt:     now,
		DryRun:              sessA.DryRun,
		CompactionEnabled:   sessA.CompactionEnabled,
		CompactionReserve:   sessA.CompactionReserve,
		CompactionKeep:      sessA.CompactionKeep,
		Messages: []types.Message{
			{Role: "user", Content: mergeUserPrompt(sessA.ID, sessB.ID)},
			{Role: "assistant", Content: synthesisText},
		},
	}
	if err := s.manager.Save(merged); err != nil {
		return nil, status.Errorf(codes.Internal, "save merged session: %v", err)
	}
	s.getOrCreate(mergedID)
	return &pb.NewSessionResponse{SessionId: mergedID}, nil
}

func (s *Service) GetSessionTree(_ context.Context, req *pb.GetSessionTreeRequest) (*pb.GetSessionTreeResponse, error) {
	if s.manager == nil {
		return nil, status.Error(codes.Unimplemented, "session persistence disabled")
	}

	currentLeaf := req.SessionId
	if currentLeaf == "" {
		// Try to find an active session if none specified
		s.mu.RLock()
		for id := range s.sessions {
			currentLeaf = id
			break
		}
		s.mu.RUnlock()
	}

	// Resolve session ID to its current active leaf (if possible)
	if currentLeaf != "" {
		s.mu.RLock()
		if entry, ok := s.sessions[currentLeaf]; ok {
			currentLeaf = entry.ag.Session().CurrentLeafID
		}
		s.mu.RUnlock()

		// If still matching the input ID, try loading it to find the last record's ID
		if currentLeaf == req.SessionId {
			if sess, err := s.manager.Load(currentLeaf); err == nil {
				currentLeaf = sess.CurrentLeafID
			}
		}
	}

	scope := session.ScopeSession
	switch req.Scope {
	case pb.GetSessionTreeRequest_PROJECT:
		scope = session.ScopeProject
	case pb.GetSessionTreeRequest_GLOBAL:
		scope = session.ScopeGlobal
	}

	roots, err := s.manager.BuildTree(currentLeaf, scope)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "build tree: %v", err)
	}

	return &pb.GetSessionTreeResponse{
		Roots: sessionTreeToProto(roots),
	}, nil
}

func sessionTreeToProto(nodes []*session.TreeNode) []*pb.SessionNode {
	res := make([]*pb.SessionNode, 0, len(nodes))
	for _, n := range nodes {
		node := &pb.SessionNode{
			SessionId:          n.ID,
			Name:               n.Name,
			FirstMessage:       n.FirstMessage,
			CreatedAt:          n.CreatedAt.Unix(),
			UpdatedAt:          n.UpdatedAt.Unix(),
			Children:           sessionTreeToProto(n.Children),
			ParentMessageIndex: -1,
			Role:               n.Role,
			Content:            n.Content,
			IsActive:           n.IsActive,
		}
		if n.ParentMessageIndex != nil {
			node.ParentMessageIndex = int32(*n.ParentMessageIndex)
		}
		if n.MergeSourceID != nil {
			node.MergeSourceId = *n.MergeSourceID
		}
		res = append(res, node)
	}
	return res
}

// ── LLM Synthesis Helpers ─────────────────────────────────────────────────────

// squashMessages condenses the selected messages from source into a single
// user+assistant pair using the LLM provider.

// buildMergeSynthesis calls the LLM to produce a synthesis of two session branches.
func (s *Service) buildMergeSynthesis(ctx context.Context, a, b *session.Session) (string, error) {
	prompt := buildMergePrompt(a, b)
	return s.llmOneShotText(ctx, a.Model, a.Provider, prompt)
}

// llmOneShotText sends a single user message to the provider and drains the stream
// into a plain text string.
func (s *Service) llmOneShotText(ctx context.Context, model, _ string, userMsg string) (string, error) {
	ch, err := s.provider.Stream(ctx, &llm.CompletionRequest{
		Model: model,
		Messages: []types.Message{
			{Role: "user", Content: userMsg},
		},
	})
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for ev := range ch {
		if ev.Error != nil {
			return "", ev.Error
		}
		if ev.Type == llm.EventTextDelta {
			sb.WriteString(ev.Content)
		}
	}
	return sb.String(), nil
}


func buildMergePrompt(a, b *session.Session) string {
	const maxMsgs = 20
	var sb strings.Builder
	sb.WriteString("I have two AI session branches that explored different approaches and I want to merge their knowledge.\n\n")

	fmt.Fprintf(&sb, "## Branch A (session %s, last %d messages)\n\n", shortID(a.ID), min(len(a.Messages), maxMsgs))
	for _, m := range last(a.Messages, maxMsgs) {
		fmt.Fprintf(&sb, "**%s:** %s\n\n", m.Role, truncate(m.Content, 300))
	}

	fmt.Fprintf(&sb, "## Branch B (session %s, last %d messages)\n\n", shortID(b.ID), min(len(b.Messages), maxMsgs))
	for _, m := range last(b.Messages, maxMsgs) {
		fmt.Fprintf(&sb, "**%s:** %s\n\n", m.Role, truncate(m.Content, 300))
	}

	sb.WriteString("Synthesize a unified context: summarise what each branch accomplished, " +
		"note any conflicts or overlaps, and provide a coherent starting point that " +
		"captures the knowledge from both branches.")
	return sb.String()
}

func mergeUserPrompt(idA, idB string) string {
	return fmt.Sprintf("Merge of session %s and session %s", shortID(idA), shortID(idB))
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func last[T any](slice []T, n int) []T {
	if len(slice) <= n {
		return slice
	}
	return slice[len(slice)-n:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── Internal Helpers ───────────────────────────────────────────────────────────

func isTerminalEvent(t agent.EventType) bool {
	return t == agent.EventAgentEnd || t == agent.EventError || t == agent.EventAbort
}

func eventToProto(sessionID string, ev agent.Event) *pb.AgentEvent {
	out := &pb.AgentEvent{SessionId: sessionID}
	switch ev.Type {
	case agent.EventAgentStart:
		out.Payload = &pb.AgentEvent_AgentStart{AgentStart: &pb.AgentStartEvent{}}
	case agent.EventTurnStart:
		out.Payload = &pb.AgentEvent_TurnStart{TurnStart: &pb.TurnStartEvent{}}
	case agent.EventMessageStart:
		out.Payload = &pb.AgentEvent_MessageStart{MessageStart: &pb.MessageStartEvent{}}
	case agent.EventTextDelta:
		out.Payload = &pb.AgentEvent_TextDelta{TextDelta: &pb.TextDeltaEvent{Content: ev.Content}}
	case agent.EventThinkingDelta:
		out.Payload = &pb.AgentEvent_ThinkingDelta{ThinkingDelta: &pb.ThinkingDeltaEvent{Content: ev.Content}}
	case agent.EventToolCall:
		if ev.ToolCall == nil {
			return nil
		}
		out.Payload = &pb.AgentEvent_ToolCall{ToolCall: &pb.ToolCallEvent{
			Id:       ev.ToolCall.ID,
			Name:     ev.ToolCall.Name,
			ArgsJson: string(ev.ToolCall.Args),
			Position: int32(ev.ToolCall.Position),
		}}
	case agent.EventToolDelta:
		toolID := ""
		if ev.ToolCall != nil {
			toolID = ev.ToolCall.ID
		}
		out.Payload = &pb.AgentEvent_ToolDelta{ToolDelta: &pb.ToolDeltaEvent{
			Content:    ev.Content,
			ToolCallId: toolID,
		}}
	case agent.EventToolOutput:
		if ev.ToolOutput == nil {
			return nil
		}
		out.Payload = &pb.AgentEvent_ToolOutput{ToolOutput: &pb.ToolOutputEvent{
			ToolCallId: ev.ToolOutput.ToolCallID,
			ToolName:   ev.ToolOutput.ToolName,
			Content:    ev.ToolOutput.Content,
			IsError:    ev.ToolOutput.IsError,
		}}
	case agent.EventMessageEnd:
		msg := &pb.MessageEndEvent{}
		if ev.Usage != nil {
			msg.PromptTokens = int32(ev.Usage.PromptTokens)
			msg.CompletionTokens = int32(ev.Usage.CompletionTokens)
			msg.TotalTokens = int32(ev.Usage.TotalTokens)
		}
		out.Payload = &pb.AgentEvent_MessageEnd{MessageEnd: msg}
	case agent.EventAgentEnd:
		out.Payload = &pb.AgentEvent_AgentEnd{AgentEnd: &pb.AgentEndEvent{}}
	case agent.EventError:
		msg := ""
		if ev.Error != nil {
			msg = ev.Error.Error()
		}
		out.Payload = &pb.AgentEvent_Error{Error: &pb.ErrorEvent{Message: msg}}
	case agent.EventAbort:
		out.Payload = &pb.AgentEvent_Abort{Abort: &pb.AbortEvent{}}
	case agent.EventStateChange:
		if ev.StateChange == nil {
			return nil
		}
		out.Payload = &pb.AgentEvent_StateChange{StateChange: &pb.StateChangeEvent{
			From: string(ev.StateChange.From),
			To:   string(ev.StateChange.To),
		}}
	case agent.EventTokens:
		out.Payload = &pb.AgentEvent_Tokens{Tokens: &pb.TokensEvent{Value: ev.Value}}
	case agent.EventHeartbeat:
		out.Payload = &pb.AgentEvent_Heartbeat{Heartbeat: &pb.HeartbeatEvent{
			Timestamp: ev.Value,
			Lifecycle: ev.Content,
		}}
	case agent.EventCompactStart:
		out.Payload = &pb.AgentEvent_CompactStart{CompactStart: &pb.CompactStartEvent{Message: ev.Content}}
	case agent.EventCompactEnd:
		out.Payload = &pb.AgentEvent_CompactEnd{CompactEnd: &pb.CompactEndEvent{Message: ev.Content}}
	case agent.EventQueueUpdate:
		out.Payload = &pb.AgentEvent_QueueUpdate{QueueUpdate: &pb.QueueUpdateEvent{}}
	default:
		return nil
	}
	return out
}
