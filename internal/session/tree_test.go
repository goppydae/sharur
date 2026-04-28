package session

import (
	"testing"
	"time"

	"github.com/goppydae/gollm/internal/types"
)

func TestBuildTree(t *testing.T) {
	mgr := newTestManager(t)

	// Create a session hierarchy
	s1, _ := mgr.Create()
	_, _ = mgr.AppendMessage(s1, types.Message{Role: "user", Content: "root"})

	s2, _ := mgr.BranchAt(s1, s1.Messages[0].ID)
	_, _ = mgr.AppendMessage(s2, types.Message{Role: "user", Content: "branch 1"})

	roots, err := mgr.BuildTree(s1.ID, ScopeSession)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	if len(roots) != 1 {
		t.Errorf("expected 1 root, got %d", len(roots))
	}

	root := roots[0]
	t.Logf("s1.ID: %s", s1.ID)
	t.Logf("s2.ID: %s", s2.ID)
	t.Logf("Root ID: %s", root.ID)
	if root.ID != s1.ID {
		t.Errorf("expected root ID %s, got %s", s1.ID, root.ID)
	}

	// s1 has two children: the first message AND the branched session s2
	if len(root.Children) != 2 {
		t.Errorf("expected 2 children for root (message and branched session), got %d", len(root.Children))
	}

	foundS2 := false
	for _, child := range root.Children {
		t.Logf("Child: ID=%s, Role=%s, Content=%s", child.ID, child.Role, child.Content)
		if child.ID == s2.ID {
			foundS2 = true
			break
		}
	}
	if !foundS2 {
		t.Errorf("branched session s2 (%s) not found in root children", s2.ID)
	}
}

func TestFlattenTree(t *testing.T) {
	root := &TreeNode{
		ID: "root",
		Children: []*TreeNode{
			{ID: "child1"},
			{ID: "child2", Children: []*TreeNode{{ID: "grandchild"}}},
		},
	}

	flat := FlattenTree([]*TreeNode{root})
	if len(flat) != 4 {
		t.Errorf("expected 4 flat nodes, got %d", len(flat))
	}
}

func TestRenderTree(t *testing.T) {
	now := time.Now()
	root := &TreeNode{
		ID:        "root-uuid-long",
		Name:      "root",
		UpdatedAt: now,
		MsgCount:  1,
		Children: []*TreeNode{
			{
				ID:        "child-uuid-long",
				Name:      "child",
				UpdatedAt: now,
				MsgCount:  2,
			},
		},
	}

	out := RenderTree([]*TreeNode{root}, "root-uuid-long")
	if !contains(out, "▶ root") {
		t.Errorf("rendered tree missing active marker or name: %s", out)
	}
	if !contains(out, "└──   child") {
		t.Errorf("rendered tree missing child or connector: %s", out)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr))))
}
