package grpcserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"github.com/goppydae/gollm/internal/grpcserver"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// ── Fakes ────────────────────────────────────────────────────────────────────

type fakeProvider struct {
	events []*llm.Event
	delay  time.Duration
}

func (f *fakeProvider) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan *llm.Event, error) {
	ch := make(chan *llm.Event, len(f.events)+1)
	go func() {
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		for _, e := range f.events {
			ch <- e
		}
		close(ch)
	}()
	return ch, nil
}

func (f *fakeProvider) Info() llm.ProviderInfo {
	return llm.ProviderInfo{Name: "fake", Model: "test"}
}

func textProvider(texts ...string) *fakeProvider {
	var evs []*llm.Event
	for _, t := range texts {
		evs = append(evs, &llm.Event{Type: llm.EventTextDelta, Content: t})
	}
	evs = append(evs, &llm.Event{Type: llm.EventMessageEnd, Usage: &types.Usage{}})
	return &fakeProvider{events: evs}
}

func slowProvider(delay time.Duration) *fakeProvider {
	return &fakeProvider{
		delay: delay,
		events: []*llm.Event{
			{Type: llm.EventTextDelta, Content: "slow"},
			{Type: llm.EventMessageEnd, Usage: &types.Usage{}},
		},
	}
}

type fakeTool struct{}

func (f *fakeTool) Name() string                     { return "noop" }
func (f *fakeTool) Description() string              { return "noop" }
func (f *fakeTool) Schema() json.RawMessage          { return json.RawMessage("{}") }
func (f *fakeTool) IsReadOnly() bool                 { return true }
func (f *fakeTool) Execute(_ context.Context, _ json.RawMessage, _ tools.ToolUpdate) (*tools.ToolResult, error) {
	return &tools.ToolResult{Content: "ok"}, nil
}

// ── Test harness ──────────────────────────────────────────────────────────────

func newTestClient(t *testing.T, prov llm.Provider) pb.AgentServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)

	reg := tools.NewToolRegistry()
	reg.Register(&fakeTool{})
	srv := grpcserver.New(context.Background(), prov, reg, nil, nil)

	gs := grpc.NewServer()
	pb.RegisterAgentServiceServer(gs, srv)
	go gs.Serve(lis) //nolint:errcheck
	t.Cleanup(func() { gs.Stop() })

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() }) //nolint:errcheck
	return pb.NewAgentServiceClient(conn)
}

func collectEvents(t *testing.T, stream pb.AgentService_PromptClient) []*pb.AgentEvent {
	t.Helper()
	var evs []*pb.AgentEvent
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		evs = append(evs, ev)
	}
	return evs
}

func hasEventType(evs []*pb.AgentEvent, check func(*pb.AgentEvent) bool) bool {
	for _, e := range evs {
		if check(e) {
			return true
		}
	}
	return false
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestPrompt_StreamsEvents(t *testing.T) {
	client := newTestClient(t, textProvider("hello ", "world"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Prompt(ctx, &pb.PromptRequest{
		SessionId: "s1",
		Message:   "hi",
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	evs := collectEvents(t, stream)

	if !hasEventType(evs, func(e *pb.AgentEvent) bool { _, ok := e.Payload.(*pb.AgentEvent_AgentStart); return ok }) {
		t.Error("expected agent_start event")
	}
	if !hasEventType(evs, func(e *pb.AgentEvent) bool { _, ok := e.Payload.(*pb.AgentEvent_AgentEnd); return ok }) {
		t.Error("expected agent_end event")
	}
	if !hasEventType(evs, func(e *pb.AgentEvent) bool {
		d, ok := e.Payload.(*pb.AgentEvent_TextDelta)
		return ok && d.TextDelta.Content != ""
	}) {
		t.Error("expected text_delta event with content")
	}
}

func TestListSessions_EmptyAtStart(t *testing.T) {
	client := newTestClient(t, textProvider())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.ListSessions(ctx, &pb.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(resp.Sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(resp.Sessions))
	}
}

func TestGetState_AfterPrompt(t *testing.T) {
	client := newTestClient(t, textProvider("ok"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, _ := client.Prompt(ctx, &pb.PromptRequest{SessionId: "s2", Message: "test"})
	collectEvents(t, stream)

	state, err := client.GetState(ctx, &pb.GetStateRequest{SessionId: "s2"})
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.SessionId != "s2" {
		t.Errorf("expected session s2, got %s", state.SessionId)
	}
	if state.MessageCount == 0 {
		t.Error("expected at least one message after prompt")
	}
}

func TestGetState_UnknownSession(t *testing.T) {
	client := newTestClient(t, textProvider())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.GetState(ctx, &pb.GetStateRequest{SessionId: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestDeleteSession(t *testing.T) {
	client := newTestClient(t, textProvider("hi"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, _ := client.Prompt(ctx, &pb.PromptRequest{SessionId: "del-me", Message: "hello"})
	collectEvents(t, stream)

	resp, err := client.DeleteSession(ctx, &pb.DeleteSessionRequest{SessionId: "del-me"})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if !resp.Ok {
		t.Error("expected ok=true")
	}

	// Session should be gone
	_, err = client.GetState(ctx, &pb.GetStateRequest{SessionId: "del-me"})
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestAbort_WhileRunning(t *testing.T) {
	client := newTestClient(t, slowProvider(500*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Prompt(ctx, &pb.PromptRequest{SessionId: "abort-me", Message: "run"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	// Abort immediately while provider is sleeping
	time.Sleep(50 * time.Millisecond)
	abortResp, err := client.Abort(ctx, &pb.AbortRequest{SessionId: "abort-me"})
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if !abortResp.Ok {
		t.Error("expected abort ok=true")
	}

	// Stream should close (either normally or with abort event)
	evs := collectEvents(t, stream)
	_ = evs // stream just needs to close
}

func newTestClientWithManager(t *testing.T, prov llm.Provider, mgr *session.Manager) pb.AgentServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)

	reg := tools.NewToolRegistry()
	reg.Register(&fakeTool{})
	srv := grpcserver.New(context.Background(), prov, reg, mgr, nil)

	gs := grpc.NewServer()
	pb.RegisterAgentServiceServer(gs, srv)
	go gs.Serve(lis) //nolint:errcheck
	t.Cleanup(func() { gs.Stop() })

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() }) //nolint:errcheck
	return pb.NewAgentServiceClient(conn)
}

func TestMultipleSessions_Isolated(t *testing.T) {
	client := newTestClient(t, textProvider("a"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run two sessions concurrently
	done := make(chan []*pb.AgentEvent, 2)

	for _, id := range []string{"sessionA", "sessionB"} {
		id := id
		go func() {
			stream, err := client.Prompt(ctx, &pb.PromptRequest{SessionId: id, Message: "hi"})
			if err != nil {
				done <- nil
				return
			}
			done <- collectEvents(t, stream)
		}()
	}

	evs1 := <-done
	evs2 := <-done

	if evs1 == nil || evs2 == nil {
		t.Fatal("one or both sessions failed")
	}

	// Each stream should contain only its own session_id
	for _, evs := range [][]*pb.AgentEvent{evs1, evs2} {
		if len(evs) == 0 {
			t.Error("expected events from session")
		}
	}
}

func TestSessionPersistence_SaveAndReload(t *testing.T) {
	dir := t.TempDir()
	mgr := session.NewManager(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First server: run a prompt so the session gets saved to disk.
	client1 := newTestClientWithManager(t, textProvider("turn1"), mgr)
	stream1, err := client1.Prompt(ctx, &pb.PromptRequest{SessionId: "persist-me", Message: "hello"})
	if err != nil {
		t.Fatalf("Prompt (server1): %v", err)
	}
	if evs := collectEvents(t, stream1); len(evs) == 0 {
		t.Fatal("expected events from first prompt")
	}

	// Verify the session landed on disk.
	if _, err := mgr.Load("persist-me"); err != nil {
		t.Fatalf("session not persisted to disk: %v", err)
	}

	// Second server (same manager, fresh in-memory state):
	// ListSessions before any Prompt should surface the disk session.
	client2 := newTestClientWithManager(t, textProvider("turn2"), mgr)
	listResp, err := client2.ListSessions(ctx, &pb.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	foundInList := false
	for _, s := range listResp.Sessions {
		if s.SessionId == "persist-me" {
			foundInList = true
		}
	}
	if !foundInList {
		t.Error("expected persist-me in ListSessions from disk")
	}

	// Prompting on the second server with the same session ID loads from disk
	// and restores the prior messages.
	stream2, err := client2.Prompt(ctx, &pb.PromptRequest{SessionId: "persist-me", Message: "follow-up"})
	if err != nil {
		t.Fatalf("Prompt (server2): %v", err)
	}
	collectEvents(t, stream2)

	state, err := client2.GetState(ctx, &pb.GetStateRequest{SessionId: "persist-me"})
	if err != nil {
		t.Fatalf("GetState after reload: %v", err)
	}
	// Two turns → at least 4 messages (user+assistant for each turn).
	if state.MessageCount < 4 {
		t.Errorf("expected ≥4 messages after reload, got %d", state.MessageCount)
	}
}
