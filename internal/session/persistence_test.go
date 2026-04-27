package session

import (
	"os"
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

func TestStore_AppendRead_Tree(t *testing.T) {
	dir := t.TempDir()
	s := newStore(dir, dir)

	id := "test-tree"
	// 1. Write session header
	r1 := record{
		Type:      TypeSession,
		ID:        "root",
		Version:   3,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.appendRecord(s.path(id), r1); err != nil {
		t.Fatalf("appendRecord r1: %v", err)
	}

	// 2. Write model change
	r2 := record{
		Type:      TypeModelChange,
		ID:        "m1",
		ParentID:  ptr("root"),
		Model:     "gpt-4",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.appendRecord(s.path(id), r2); err != nil {
		t.Fatalf("appendRecord r2: %v", err)
	}

	// 3. Write message
	r3 := record{
		Type: TypeMessage,
		ID:   "msg1",
		ParentID: ptr("m1"),
		Message: &types.Message{
			Role:    "user",
			Content: "hello",
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.appendRecord(s.path(id), r3); err != nil {
		t.Fatalf("appendRecord r3: %v", err)
	}

	// 4. Read back
	records, err := s.readPath(s.path(id))
	if err != nil {
		t.Fatalf("readPath: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if records[0].ID != "root" || records[1].Model != "gpt-4" || records[2].Message.Content != "hello" {
		t.Errorf("records content mismatch")
	}
}

func TestStore_TimestampedFilename(t *testing.T) {
	dir := t.TempDir()
	s := newStore(dir, dir)

	id := "abc-123"
	r := record{Type: TypeSession, ID: "root"}
	if err := s.appendRecord(s.path(id), r); err != nil {
		t.Fatal(err)
	}

	// File should exist and include the session ID in its name.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if !strings.Contains(entries[0].Name(), "abc-123.jsonl") {
		t.Errorf("filename %q does not contain session ID", entries[0].Name())
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

	// Sessions are not persisted until the first Append* call.
	if _, err := mgr.Load(sess.ID); err == nil {
		t.Error("expected Load to fail for a session with no content")
	}

	// Appending a message persists the session; Load should now succeed.
	if _, err := mgr.AppendMessage(sess, types.Message{Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	loaded, err := mgr.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load after AppendMessage: %v", err)
	}
	if loaded.ID != sess.ID {
		t.Errorf("Load returned wrong ID: %q", loaded.ID)
	}
}

func TestManager_AppendMessage_SyncsMessages(t *testing.T) {
	mgr := newTestManager(t)
	sess, _ := mgr.Create()

	msg := types.Message{Role: "user", Content: "howdy"}
	rid, err := mgr.AppendMessage(sess, msg)
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if rid == "" {
		t.Fatal("AppendMessage returned empty record ID")
	}

	if len(sess.Messages) != 1 || sess.Messages[0].Content != "howdy" {
		t.Errorf("sess.Messages not updated: %v", sess.Messages)
	}

	// Load fresh from disk
	got, _ := mgr.Load(sess.ID)
	if len(got.Messages) != 1 || got.Messages[0].Content != "howdy" {
		t.Errorf("loaded Messages mismatch: %v", got.Messages)
	}
	if got.Messages[0].ID != rid {
		t.Errorf("message ID mismatch: got %q, want %q", got.Messages[0].ID, rid)
	}
}

func TestManager_BranchAt(t *testing.T) {
	mgr := newTestManager(t)
	sess, _ := mgr.Create()

	m1ID, _ := mgr.AppendMessage(sess, types.Message{Role: "user", Content: "msg 1"})
	if _, err := mgr.AppendMessage(sess, types.Message{Role: "assistant", Content: "msg 2"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Branch at first message
	branched, err := mgr.BranchAt(sess, m1ID)
	if err != nil {
		t.Fatalf("BranchAt: %v", err)
	}

	if branched.ID == sess.ID {
		t.Fatal("branched session should have a new ID")
	}
	if len(branched.Messages) != 1 || branched.Messages[0].Content != "msg 1" {
		t.Fatalf("branched messages wrong: %v", branched.Messages)
	}

	// Branched session should have parentSession in its header
	records, _ := mgr.store.readPath(mgr.store.path(branched.ID))
	if records[0].ParentSession == nil || *records[0].ParentSession != sess.ID {
		t.Errorf("branched header missing ParentSession: %v", records[0])
	}
}

func TestManager_ToTypes(t *testing.T) {
	mgr := newTestManager(t)
	sess, _ := mgr.Create()
	_ = mgr.AppendModelChange(sess, "anthropic", "llama3")
	_, _ = mgr.AppendMessage(sess, types.Message{Role: "user", Content: "hi"})

	ts := sess.ToTypes()
	if ts.Model != "llama3" {
		t.Errorf("ToTypes Model: %q", ts.Model)
	}
	if len(ts.Messages) != 1 || ts.Messages[0].Content != "hi" {
		t.Errorf("ToTypes Messages: %v", ts.Messages)
	}
}

