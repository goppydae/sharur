// Package session provides JSONL-backed session management.
package session

import (
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

// Session holds a conversation with all its metadata.
type Session struct {
	ID           string    `json:"id"`
	ParentID     *string   `json:"parentId,omitempty"`
	Name         string    `json:"name,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Model        string    `json:"model,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	Thinking     string    `json:"thinkingLevel,omitempty"`
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	Messages     []message `json:"messages,omitempty"`
}

// SessionSummary provides a lightweight view of a session for listings.
type SessionSummary struct {
	ID           string
	ParentID     *string
	Name         string
	FirstMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ToTypes converts a session.Session to types.Session.
func (s *Session) ToTypes() *types.Session {
	return &types.Session{
		ID:           s.ID,
		ParentID:     s.ParentID,
		Name:         s.Name,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		Model:        s.Model,
		Provider:     s.Provider,
		Thinking:     types.ThinkingLevel(s.Thinking),
		SystemPrompt: s.SystemPrompt,
		Messages:     s.Messages,
	}
}

// Manager creates, lists, loads, and saves sessions.
type Manager struct {
	dir   string
	store *store
	mu    sync.RWMutex
}

// NewManager creates a new session manager. It organizes sessions into
// subdirectories based on a sanitized version of the current working directory.
func NewManager(baseDir string) *Manager {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".gollm", "sessions")
	}

	cwd, _ := os.Getwd()
	projectDir := filepath.Join(baseDir, projectPath(cwd))

	return &Manager{
		dir:   projectDir,
		store: newStore(projectDir),
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
	m.mu.Lock()
	defer m.mu.Unlock()

	sess := &Session{
		ID:        generateID(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := m.store.write(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// Load retrieves a session by ID.
func (m *Manager) Load(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.read(id)
}

// LoadPath retrieves a session from a raw file path.
func (m *Manager) LoadPath(path string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.readPath(path)
}

// Save persists an existing session.
func (m *Manager) Save(sess *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess.UpdatedAt = time.Now()
	return m.store.write(sess)
}

// SavePath persists a session to a specific file path.
func (m *Manager) SavePath(sess *Session, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess.UpdatedAt = time.Now()
	return m.store.writePath(sess, path)
}

// List returns the IDs of all stored sessions.
func (m *Manager) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.list()
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

// Fork creates a new session branching off a source session at parentID.
// The new session shares the source's messages and metadata, with ParentID set.
func (m *Manager) Fork(source *Session) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	newID := generateID()
	forked := &Session{
		ID:       newID,
		ParentID: &source.ID,
		Name:     source.Name, // Keep name from source or empty
	}
	m.copyFrom(forked, source)

	if err := m.store.write(forked); err != nil {
		return nil, err
	}
	return forked, nil
}

// Clone duplicates a session into a brand-new session with no parent link.
func (m *Manager) Clone(source *Session) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cloned := &Session{
		ID:   generateID(),
		Name: source.Name,
	}
	m.copyFrom(cloned, source)

	if err := m.store.write(cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

// copyFrom populates target metadata and messages from source.
// It does NOT copy ID, ParentID, or Name.
func (m *Manager) copyFrom(target, source *Session) {
	target.Model = source.Model
	target.Provider = source.Provider
	target.Thinking = source.Thinking
	target.SystemPrompt = source.SystemPrompt
	target.CreatedAt = time.Now()
	target.UpdatedAt = time.Now()
	target.Messages = make([]message, len(source.Messages))
	copy(target.Messages, source.Messages)
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
