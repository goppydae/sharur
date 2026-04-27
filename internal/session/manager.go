// Package session provides JSONL-backed session management.
package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/goppydae/gollm/internal/types"
)

// message is an alias so we can use types.Message in JSONL without an import cycle.
type message = types.Message

// Session holds a conversation tree and its metadata.
type Session struct {
	ID        string    `json:"id"` // Root session ID (also the filename base)
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Tree state
	CurrentLeafID string   `json:"currentLeafId"`
	Records       []record `json:"-"` // All records in the file

	// Merged state for the current leaf
	Name               string    `json:"name,omitempty"`
	Model              string    `json:"model,omitempty"`
	Provider           string    `json:"provider,omitempty"`
	Thinking           string    `json:"thinkingLevel,omitempty"`
	SystemPrompt       string    `json:"systemPrompt,omitempty"`
	Messages           []message `json:"messages,omitempty"`
	ParentID           *string   `json:"parentId,omitempty"`
	ParentMessageIndex *int      `json:"parentMessageIndex,omitempty"`
	MergeSourceID      *string   `json:"mergeSourceId,omitempty"`
	RebasedFrom        *string   `json:"rebasedFrom,omitempty"`

	// Config (inherited or session-wide)
	DryRun            bool `json:"dryRun,omitempty"`
	CompactionEnabled bool `json:"compactionEnabled,omitempty"`
	CompactionReserve int  `json:"compactionReserveTokens,omitempty"`
	CompactionKeep    int  `json:"compactionKeepRecentTokens,omitempty"`
	LatestCompaction  *types.CompactionState `json:"latestCompaction,omitempty"`
}

// SessionSummary provides a lightweight view of a session for listings.
type SessionSummary struct {
	ID           string
	ParentID     *string
	Name         string
	FirstMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// Lineage metadata
	ParentMessageIndex *int
	MergeSourceID      *string
	RebasedFrom        *string
}

// ToTypes converts a session.Session to types.Session.
func (s *Session) ToTypes() *types.Session {
	ts := &types.Session{
		ID:           s.ID,
		Name:         s.Name,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		Model:        s.Model,
		Provider:     s.Provider,
		Thinking:     types.ThinkingLevel(s.Thinking),
		SystemPrompt: s.SystemPrompt,
		Messages:     s.Messages,
		DryRun:            s.DryRun,
		CompactionEnabled:   s.CompactionEnabled,
		CompactionReserve:   s.CompactionReserve,
		CompactionKeep:      s.CompactionKeep,
		LatestCompaction:    s.LatestCompaction,
	}
	
	// Find ParentID from header record if available
	for _, r := range s.Records {
		if r.Type == TypeSession && r.ParentSession != nil {
			ts.ParentID = r.ParentSession
			break
		}
	}
	
	return ts
}

// Manager creates, lists, loads, and saves sessions.
type Manager struct {
	baseDir string
	dir     string
	store   *store
	mu      sync.RWMutex
}

// NewManager creates a new session manager. It organizes sessions into
// subdirectories based on a sanitized version of the current working directory.
func NewManager(baseDir string) *Manager {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".gollm", "sessions")
	}

	cwd, err := os.Getwd()
	if err != nil {
		// Fall back to home directory so session storage is at least predictable.
		home, _ := os.UserHomeDir()
		cwd = home
	}
	// Resolve symlinks so the same project accessed via different paths maps to
	// the same session directory.
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	projectDir := filepath.Join(baseDir, projectPath(cwd))

	return &Manager{
		baseDir: baseDir,
		dir:     projectDir,
		store:   newStore(baseDir, projectDir),
	}
}

func projectPath(cwd string) string {
	cwd = filepath.Clean(cwd)
	// Replace separators with dashes
	p := strings.ReplaceAll(cwd, string(filepath.Separator), "-")
	// Ensure it starts and ends with double dashes as requested
	return "--" + strings.Trim(p, "-") + "--"
}

// Create allocates a new empty session and persists it.
func (m *Manager) Create() (*Session, error) {
	return m.CreateWithID(generateID())
}

// CreateWithID allocates a new empty session with the given ID and persists it.
func (m *Manager) CreateWithID(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	header := record{
		Type:      TypeSession,
		ID:        id,
		Version:   3,
		Timestamp: now.UTC().Format(time.RFC3339Nano),
	}

	// Session file is not written until the first Append* call (lazy creation).
	return &Session{
		ID:            id,
		CreatedAt:     now,
		UpdatedAt:     now,
		CurrentLeafID: id,
		Records:       []record{header},
	}, nil
}

// ensureFile initialises the session file if it does not yet exist.
// Must be called with m.mu held for writing. Returns the resolved path.
func (m *Manager) ensureFile(sess *Session) (string, error) {
	return m.store.initFile(sess.ID, sess.CreatedAt, 3)
}

// Load loads a session from its file ID.
func (m *Manager) Load(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. Try local project first.
	records, err := m.store.read(id)
	if err != nil {
		// 2. Try scanning other projects.
		entries, _ := os.ReadDir(m.baseDir)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			pdir := filepath.Join(m.baseDir, e.Name())
			if pdir == m.dir {
				continue
			}
			ps := newStore(m.baseDir, pdir)
			records, err = ps.read(id)
			if err == nil {
				break
			}
		}
	}

	if err != nil {
		return nil, err
	}

	sess := &Session{
		ID:      id,
		Records: records,
	}

	// Find the most recent leaf (the last message or the last record in the file)
	var lastLeafID string
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].ID != "" {
			lastLeafID = records[i].ID
			break
		}
	}

	if err := sess.buildSessionContext(lastLeafID); err != nil {
		return nil, err
	}

	return sess, nil
}

// LoadForRecord finds and loads the session file containing the specified record ID.
func (m *Manager) LoadForRecord(recordID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normID := strings.TrimSpace(strings.ToLower(recordID))

	// Get all project directories
	entries, _ := os.ReadDir(m.baseDir)
	var projectDirs []string
	for _, e := range entries {
		if e.IsDir() {
			projectDirs = append(projectDirs, filepath.Join(m.baseDir, e.Name()))
		}
	}
	if len(projectDirs) == 0 {
		projectDirs = []string{m.dir}
	}

	for _, pdir := range projectDirs {
		ps := newStore(m.baseDir, pdir)
		ids, err := ps.list()
		if err != nil {
			continue
		}
		for _, fileID := range ids {
			records, err := ps.read(fileID)
			if err != nil {
				continue
			}
			for _, r := range records {
				if strings.TrimSpace(strings.ToLower(r.ID)) == normID {
					// Found it! Load this file.
					sess := &Session{
						ID:      fileID,
						Records: records,
					}
					_ = sess.buildSessionContext(r.ID)
					return sess, nil
				}
			}
		}
	}
	return nil, os.ErrNotExist
}

// LoadPath retrieves a session from a raw file path.
func (m *Manager) LoadPath(path string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	records, err := m.store.readPath(path)
	if err != nil {
		return nil, err
	}

	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if i := strings.Index(id, "_"); i != -1 {
		id = id[i+1:]
	}

	sess := &Session{
		ID:      id,
		Records: records,
	}

	var lastLeafID string
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].ID != "" {
			lastLeafID = records[i].ID
			break
		}
	}

	if err := sess.buildSessionContext(lastLeafID); err != nil {
		return nil, err
	}

	return sess, nil
}

// List returns the IDs of all stored sessions.
func (m *Manager) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.list()
}

// LatestWithMessages returns the ID of the session whose most recent message
// has the latest timestamp. Returns "", nil if no session with messages exists.
// This is used by --continue.
func (m *Manager) LatestWithMessages() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids, err := m.store.list()
	if err != nil {
		return "", err
	}
	var best string
	var bestTime time.Time
	for _, id := range ids {
		sum, err := m.store.readSummary(id)
		if err != nil || sum.FirstMessage == "" {
			continue
		}
		if sum.UpdatedAt.After(bestTime) {
			bestTime = sum.UpdatedAt
			best = id
		}
	}
	return best, nil
}

// ListSummaries returns summaries of all stored sessions.
func (m *Manager) ListSummaries() ([]SessionSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids, err := m.store.list()
	if err != nil {
		return nil, err
	}

	var summaries []SessionSummary
	for _, id := range ids {
		sum, err := m.store.readSummary(id)
		if err == nil {
			summaries = append(summaries, *sum)
		}
	}
	return summaries, nil
}

// SavePath persists a session to a specific file path.
func (m *Manager) SavePath(sess *Session, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	var records []record
	// Add header
	records = append(records, record{
		Type:      TypeSession,
		ID:        sess.ID,
		Version:   3,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})
	
	// Add messages as records
	for _, msg := range sess.Messages {
		records = append(records, record{
			Type:      TypeMessage,
			ID:        msg.ID,
			Message:   &msg,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	
	return m.store.writePath(records, path)
}

// buildSessionContext walks from the leaf to the root and populates the session state.
func (s *Session) buildSessionContext(leafID string) error {
	byID := make(map[string]record)
	for _, r := range s.Records {
		if r.ID != "" {
			byID[r.ID] = r
		}
	}

	var path []record
	curr := leafID
	visited := make(map[string]bool)
	for curr != "" {
		if visited[curr] {
			break // Cycle detected
		}
		visited[curr] = true

		r, ok := byID[curr]
		if !ok {
			break
		}
		path = append(path, r)
		if r.ParentID == nil {
			break
		}
		curr = *r.ParentID
	}

	// Reverse path to root -> leaf
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	s.Messages = nil
	s.CurrentLeafID = leafID

	for _, r := range path {
		switch r.Type {
		case TypeSession:
			if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
				s.CreatedAt = t
			}
		case TypeSessionInfo:
			s.Name = r.Name
		case TypeModelChange:
			s.Provider = r.Provider
			s.Model = r.Model
		case TypeThinkingLevelChange:
			s.Thinking = r.ThinkingLevel
		case TypeMessage:
			if r.Message != nil {
				msg := *r.Message
				msg.ID = r.ID
				s.Messages = append(s.Messages, msg)
			}
		case TypeCompaction:
			// Track the latest compaction to inform the LLM context boundary
			s.LatestCompaction = &types.CompactionState{
				Summary:          r.Summary,
				FirstKeptEntryID: r.FirstKeptEntryID,
			}
			// Add a concise notice for the history/TUI
			freed := r.TokensBefore - r.TokensAfter
			if freed < 0 {
				freed = 0
			}
			s.Messages = append(s.Messages, types.Message{
				Role:    "compaction",
				Content: fmt.Sprintf("Context compacted. Freed %d tokens.", freed),
			})
		}
		
		if r.ParentID != nil {
			s.ParentID = r.ParentID
		}
		
		if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
			s.UpdatedAt = t
		}
	}

	return nil
}

// AppendMessage adds a message to the session.
func (m *Manager) AppendMessage(sess *Session, msg message) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := generateID()
	var parentID *string
	if sess.CurrentLeafID != "" {
		parentID = ptr(sess.CurrentLeafID)
	}

	r := record{
		Type:      TypeMessage,
		ID:        id,
		ParentID:  parentID,
		Message:   &msg,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	path, err := m.ensureFile(sess)
	if err != nil {
		return "", err
	}
	if err := m.store.appendRecord(path, r); err != nil {
		return "", err
	}

	sess.Records = append(sess.Records, r)
	sess.CurrentLeafID = id
	sess.Messages = append(sess.Messages, msg)
	sess.UpdatedAt = time.Now()

	return id, nil
}

// AppendModelChange records a model change in the session.
func (m *Manager) AppendModelChange(sess *Session, provider, model string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := generateID()
	r := record{
		Type:      TypeModelChange,
		ID:        id,
		ParentID:  ptr(sess.CurrentLeafID),
		Provider:  provider,
		Model:     model,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	path, err := m.ensureFile(sess)
	if err != nil {
		return err
	}
	if err := m.store.appendRecord(path, r); err != nil {
		return err
	}

	sess.Records = append(sess.Records, r)
	sess.CurrentLeafID = id
	sess.Provider = provider
	sess.Model = model
	sess.UpdatedAt = time.Now()
	return nil
}

// AppendThinkingLevelChange records a thinking level change in the session.
func (m *Manager) AppendThinkingLevelChange(sess *Session, level string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := generateID()
	r := record{
		Type:          TypeThinkingLevelChange,
		ID:            id,
		ParentID:      ptr(sess.CurrentLeafID),
		ThinkingLevel: level,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
	}

	path, err := m.ensureFile(sess)
	if err != nil {
		return err
	}
	if err := m.store.appendRecord(path, r); err != nil {
		return err
	}

	sess.Records = append(sess.Records, r)
	sess.CurrentLeafID = id
	sess.Thinking = level
	sess.UpdatedAt = time.Now()
	return nil
}

// AppendCompaction records a compaction event in the session.
func (m *Manager) AppendCompaction(sess *Session, summary string, firstKeptEntryID string, tokensBefore, tokensAfter int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := generateID()
	r := record{
		Type:             TypeCompaction,
		ID:               id,
		ParentID:         ptr(sess.CurrentLeafID),
		Summary:          summary,
		FirstKeptEntryID: firstKeptEntryID,
		TokensBefore:     tokensBefore,
		TokensAfter:      tokensAfter,
		Timestamp:        time.Now().UTC().Format(time.RFC3339Nano),
	}

	path, err := m.ensureFile(sess)
	if err != nil {
		return err
	}
	if err := m.store.appendRecord(path, r); err != nil {
		return err
	}

	sess.Records = append(sess.Records, r)
	sess.CurrentLeafID = id
	sess.LatestCompaction = &types.CompactionState{
		Summary:          summary,
		FirstKeptEntryID: firstKeptEntryID,
	}
	sess.UpdatedAt = time.Now()
	return sess.buildSessionContext(id)
}

// AppendSessionInfo records a session name change.
func (m *Manager) AppendSessionInfo(sess *Session, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := generateID()
	r := record{
		Type:      TypeSessionInfo,
		ID:        id,
		ParentID:  ptr(sess.CurrentLeafID),
		Name:      name,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	path, err := m.ensureFile(sess)
	if err != nil {
		return err
	}
	if err := m.store.appendRecord(path, r); err != nil {
		return err
	}

	sess.Records = append(sess.Records, r)
	sess.CurrentLeafID = id
	sess.Name = name
	sess.UpdatedAt = time.Now()
	return nil
}

// BranchAt creates a new child branch from source at the given leaf ID.
func (m *Manager) BranchAt(source *Session, leafID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// In the new model, branching is just moving the leaf pointer
	// and potentially adding a new leaf if we want to diverge.
	// But to match the previous behavior of creating a NEW file:
	newID := generateID()
	
	// Create new session file with copies of records up to leafID
	var records []record
	byID := make(map[string]record)
	for _, r := range source.Records {
		if r.ID != "" {
			byID[r.ID] = r
		}
	}

	curr := leafID
	var path []record
	visited := make(map[string]bool)
	for curr != "" {
		if visited[curr] {
			break // Cycle detected
		}
		visited[curr] = true

		r, ok := byID[curr]
		if !ok {
			break
		}
		path = append(path, r)
		if r.ParentID == nil {
			break
		}
		curr = *r.ParentID
	}
	
	// Add root header
	for _, r := range source.Records {
		if r.Type == TypeSession {
			r.ParentSession = ptr(source.ID)
			records = append(records, r)
			break
		}
	}

	// Reverse path and add to records
	for i := len(path) - 1; i >= 0; i-- {
		records = append(records, path[i])
	}

	if err := m.store.write(newID, records); err != nil {
		return nil, err
	}

	sess := &Session{
		ID:      newID,
		Records: records,
	}
	_ = sess.buildSessionContext(leafID)

	return sess, nil
}

// Save persists the merged session state (mostly metadata updates).
func (m *Manager) Save(sess *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// If we changed Name or other session-wide info, we might need to append a session_info record.
	// For now, we assume changes are made via Append* methods.
	return nil
}

// Rebase creates a fresh-root session containing only the specified records.
func (m *Manager) Rebase(source *Session, recordIDs []string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	newID := generateID()
	var records []record
	
	// Add header
	for _, r := range source.Records {
		if r.Type == TypeSession {
			r.ID = newID
			records = append(records, r)
			break
		}
	}

	byID := make(map[string]record)
	for _, r := range source.Records {
		if r.ID != "" {
			byID[r.ID] = r
		}
	}

	for _, id := range recordIDs {
		if r, ok := byID[id]; ok {
			records = append(records, r)
		}
	}

	if err := m.store.write(newID, records); err != nil {
		return nil, err
	}

	sess := &Session{
		ID:      newID,
		Records: records,
	}
	if len(recordIDs) > 0 {
		_ = sess.buildSessionContext(recordIDs[len(recordIDs)-1])
	}

	return sess, nil
}

// Fork duplicates a session into a brand-new independent session with no parent link.
func (m *Manager) Fork(source *Session) (*Session, error) {
	return m.BranchAt(source, source.CurrentLeafID)
}

// Delete removes a session by ID.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	path := m.store.path(id)
	return os.Remove(path)
}

// SessionPath returns the absolute path to a session's data file.
func (m *Manager) SessionPath(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.path(id)
}

func generateID() string {
	return uuid.New().String()
}

func ptr[T any](v T) *T {
	return &v
}

