package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goppydae/gollm/internal/types"
)

// newTestManager returns a Manager whose data lives in t.TempDir() so each
// test is isolated and cleans up automatically.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(t.TempDir())
}

// ---------- store (JSONL) round-trip ----------

func TestStore_WriteRead_PreservesHeader(t *testing.T) {
	dir := t.TempDir()
	s := newStore(dir)

	parentID := "parent-uuid"
	sess := &Session{
		ID:           "test-id",
		ParentID:     &parentID,
		Name:         "My Session",
		Model:        "claude-sonnet-4-6",
		Provider:     "anthropic",
		Thinking:     "medium",
		SystemPrompt: "You are helpful.",
		CreatedAt:    time.Now().Truncate(time.Millisecond),
		UpdatedAt:    time.Now().Truncate(time.Millisecond),
	}
	if err := s.write(sess); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := s.read(sess.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if got.ID != sess.ID {
		t.Errorf("ID: got %q, want %q", got.ID, sess.ID)
	}
	if got.Name != sess.Name {
		t.Errorf("Name: got %q, want %q", got.Name, sess.Name)
	}
	if got.Model != sess.Model {
		t.Errorf("Model: got %q, want %q", got.Model, sess.Model)
	}
	if got.Provider != sess.Provider {
		t.Errorf("Provider: got %q, want %q", got.Provider, sess.Provider)
	}
	if got.Thinking != sess.Thinking {
		t.Errorf("Thinking: got %q, want %q", got.Thinking, sess.Thinking)
	}
	if got.SystemPrompt != sess.SystemPrompt {
		t.Errorf("SystemPrompt: got %q, want %q", got.SystemPrompt, sess.SystemPrompt)
	}
	if got.ParentID == nil || *got.ParentID != parentID {
		t.Errorf("ParentID: got %v, want %q", got.ParentID, parentID)
	}
	if got.CreatedAt.UnixMilli() != sess.CreatedAt.UnixMilli() {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, sess.CreatedAt)
	}
}

func TestStore_WriteRead_PreservesMessages(t *testing.T) {
	dir := t.TempDir()
	s := newStore(dir)

	rawArgs, _ := json.Marshal(map[string]any{"path": "/tmp/test.go"})
	sess := &Session{
		ID:        "msg-test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages: []message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "World", Thinking: "let me think"},
			{
				Role: "assistant",
				ToolCalls: []types.ToolCall{
					{ID: "call-1", Name: "read", Args: rawArgs},
				},
			},
			{Role: "tool", Content: "file contents", ToolCallID: "call-1"},
		},
	}
	if err := s.write(sess); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := s.read(sess.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(got.Messages) != len(sess.Messages) {
		t.Fatalf("message count: got %d, want %d", len(got.Messages), len(sess.Messages))
	}
	for i, want := range sess.Messages {
		g := got.Messages[i]
		if g.Role != want.Role {
			t.Errorf("msg[%d].Role: got %q, want %q", i, g.Role, want.Role)
		}
		if g.Content != want.Content {
			t.Errorf("msg[%d].Content: got %q, want %q", i, g.Content, want.Content)
		}
	}
	// Tool call round-trip
	if got.Messages[2].ToolCalls[0].Name != "read" {
		t.Errorf("tool call name not preserved: %v", got.Messages[2].ToolCalls)
	}
	// Tool result round-trip
	if got.Messages[3].ToolCallID != "call-1" {
		t.Errorf("ToolCallID not preserved: %q", got.Messages[3].ToolCallID)
	}
}

func TestStore_TimestampedFilename(t *testing.T) {
	dir := t.TempDir()
	s := newStore(dir)

	sess := &Session{ID: "abc-123", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.write(sess); err != nil {
		t.Fatal(err)
	}

	// File should exist and include the UUID in its name.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), "_abc-123.jsonl") {
		t.Errorf("filename %q does not contain session ID with timestamp prefix", entries[0].Name())
	}
}

func TestStore_UpdateOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	s := newStore(dir)

	sess := &Session{ID: "update-me", CreatedAt: time.Now(), UpdatedAt: time.Now(), Name: "v1"}
	if err := s.write(sess); err != nil {
		t.Fatal(err)
	}

	sess.Name = "v2"
	sess.UpdatedAt = time.Now()
	if err := s.write(sess); err != nil {
		t.Fatal(err)
	}

	// Still only one file.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file after update, got %d", len(entries))
	}

	got, err := s.read(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "v2" {
		t.Errorf("expected updated name 'v2', got %q", got.Name)
	}
}

// ---------- Manager CRUD ----------

func TestManager_CreateLoad(t *testing.T) {
	mgr := newTestManager(t)

	sess, err := mgr.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("Create returned empty ID")
	}

	loaded, err := mgr.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != sess.ID {
		t.Errorf("Load returned wrong ID: %q", loaded.ID)
	}
}

func TestManager_SaveRoundTrip(t *testing.T) {
	mgr := newTestManager(t)

	sess, err := mgr.Create()
	if err != nil {
		t.Fatal(err)
	}

	sess.Name = "persisted name"
	sess.Model = "llama3"
	sess.SystemPrompt = "Be concise."
	sess.Messages = []message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer"},
	}

	if err := mgr.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := mgr.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got.Name != "persisted name" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.Model != "llama3" {
		t.Errorf("Model: got %q", got.Model)
	}
	if got.SystemPrompt != "Be concise." {
		t.Errorf("SystemPrompt: got %q", got.SystemPrompt)
	}
	if len(got.Messages) != 2 {
		t.Errorf("Messages count: got %d, want 2", len(got.Messages))
	}
}

func TestManager_LoadPath(t *testing.T) {
	mgr := newTestManager(t)

	sess, err := mgr.Create()
	if err != nil {
		t.Fatal(err)
	}
	sess.Name = "path-load-test"
	if err := mgr.Save(sess); err != nil {
		t.Fatal(err)
	}

	path := mgr.SessionPath(sess.ID)
	if path == "" {
		t.Fatal("SessionPath returned empty string")
	}

	got, err := mgr.LoadPath(path)
	if err != nil {
		t.Fatalf("LoadPath: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, sess.ID)
	}
}

func TestManager_ToTypes(t *testing.T) {
	mgr := newTestManager(t)

	sess, _ := mgr.Create()
	sess.Model = "llama3"
	sess.Thinking = "high"
	sess.Messages = []message{{Role: "user", Content: "hi"}}
	_ = mgr.Save(sess)

	loaded, _ := mgr.Load(sess.ID)
	ts := loaded.ToTypes()

	if ts.Model != "llama3" {
		t.Errorf("ToTypes Model: %q", ts.Model)
	}
	if ts.Thinking != types.ThinkingHigh {
		t.Errorf("ToTypes Thinking: %q", ts.Thinking)
	}
	if len(ts.Messages) != 1 || ts.Messages[0].Content != "hi" {
		t.Errorf("ToTypes Messages: %v", ts.Messages)
	}
}

func TestManager_LoadNonExistent(t *testing.T) {
	mgr := newTestManager(t)

	_, err := mgr.Load("does-not-exist")
	if err == nil {
		t.Error("expected error loading non-existent session, got nil")
	}
}

// ---------- projectPath ----------

func TestProjectPath_Sanitizes(t *testing.T) {
	p := projectPath("/home/user/my-project")
	if !strings.HasPrefix(p, "--") || !strings.HasSuffix(p, "--") {
		t.Errorf("expected double-dash wrapping, got %q", p)
	}
	// Must not contain path separators.
	if strings.Contains(p, string(filepath.Separator)) {
		t.Errorf("projectPath still contains separator: %q", p)
	}
}
