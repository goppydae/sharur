package contextfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscover(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a nested structure
	// tmpDir/AGENTS.md
	// tmpDir/subdir/CLAUDE.md
	
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("outer"), 0644); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "CLAUDE.md"), []byte("inner"), 0644); err != nil {
		t.Fatal(err)
	}

	found := Discover(subdir)
	
	// Should find both, outer first
	if len(found) < 2 {
		t.Errorf("expected at least 2 context files, got %d", len(found))
	}

	foundOuter := false
	foundInner := false
	for _, f := range found {
		if filepath.Base(f) == "AGENTS.md" {
			foundOuter = true
		}
		if filepath.Base(f) == "CLAUDE.md" {
			foundInner = true
		}
	}

	if !foundOuter || !foundInner {
		t.Errorf("did not find both files: outer=%v, inner=%v", foundOuter, foundInner)
	}
}

func TestLoad(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("context content"), 0644); err != nil {
		t.Fatal(err)
	}

	loaded := Load(tmpDir)
	if !strings.Contains(loaded, "context content") {
		t.Error("expected loaded content to contain file content")
	}
	if !strings.Contains(loaded, "context: ") {
		t.Error("expected loaded content to contain source comment")
	}
}
