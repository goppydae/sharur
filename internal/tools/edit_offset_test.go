package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestEditLiteralOffset_PreservesPrefix is the regression test for the critical bug where
// editLiteral with offset > 0 returned only the suffix, discarding all lines above the offset.
func TestEditLiteralOffset_PreservesPrefix(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5"

	// Replace "line4" starting search from line 3 (offset 3).
	got, err := editLiteral(content, "line4", "REPLACED", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// prefix (lines 1-2) must be present
	if !strings.Contains(got, "line1") {
		t.Errorf("prefix 'line1' missing from result:\n%s", got)
	}
	if !strings.Contains(got, "line2") {
		t.Errorf("prefix 'line2' missing from result:\n%s", got)
	}
	// the replacement must have happened
	if !strings.Contains(got, "REPLACED") {
		t.Errorf("replacement not applied:\n%s", got)
	}
	// original target must be gone
	if strings.Contains(got, "line4") {
		t.Errorf("original text 'line4' still present:\n%s", got)
	}
}

func TestEditLiteralOffset_TextNotInSuffix(t *testing.T) {
	content := "line1\nline2\nline3"

	// "line1" exists only above offset 2 — should not be found.
	_, err := editLiteral(content, "line1", "X", 2)
	if err == nil {
		t.Fatal("expected error when target text is above the offset, got nil")
	}
}

func TestEditLiteralNoOffset_Unchanged(t *testing.T) {
	content := "hello world"
	got, err := editLiteral(content, "world", "Go", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello Go" {
		t.Errorf("expected 'hello Go', got %q", got)
	}
}

func TestEditRegexOffset_PreservesPrefix(t *testing.T) {
	content := "alpha\nbeta\ngamma\nalpha again"

	// Replace "alpha" only within the suffix starting at line 3.
	got, err := editRegex(content, `alpha`, "REPLACED", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(got, "alpha\n") {
		t.Errorf("first 'alpha' (above offset) should be preserved:\n%s", got)
	}
	if !strings.Contains(got, "REPLACED") {
		t.Errorf("replacement not applied:\n%s", got)
	}
}

func TestEditRegexOffset_PatternNotInSuffix(t *testing.T) {
	content := "line1\nline2\nline3"

	// "line1" is above offset 2.
	_, err := editRegex(content, `line1`, "X", 2)
	if err == nil {
		t.Fatal("expected error when pattern not in suffix, got nil")
	}
}

// TestEditTool_OffsetEndToEnd runs the full tool.Execute path with an offset parameter
// and verifies the written file contains both prefix and the replaced text.
func TestEditTool_OffsetEndToEnd(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "edit-offset-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	original := "header line\nsecond line\ntarget line\nfooter line"
	if _, err := tmp.WriteString(original); err != nil {
		t.Fatal(err)
	}
	_ = tmp.Close()

	tool := Edit{}
	args, _ := json.Marshal(map[string]any{
		"path":    tmp.Name(),
		"oldText": "target line",
		"newText": "replaced line",
		"offset":  3, // start search at line 3
	})

	_, err = tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	if !strings.Contains(got, "header line") {
		t.Errorf("prefix 'header line' missing:\n%s", got)
	}
	if !strings.Contains(got, "replaced line") {
		t.Errorf("replacement missing:\n%s", got)
	}
	if strings.Contains(got, "target line") {
		t.Errorf("original target still present:\n%s", got)
	}
}
