package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestGrep_OutputFormat verifies that match lines are formatted as "N: content"
// (line number, colon, space, then the content) — regression test for the missing space.
func TestGrep_OutputFormat(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("alpha\nbeta\ngamma"), 0644)

	tool := Grep{}
	args, _ := json.Marshal(map[string]any{
		"pattern": "beta",
		"path":    filepath.Join(tmpDir, "test.txt"),
	})
	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Must contain "2: beta" — line number, colon, space, then content.
	if !strings.Contains(result.Content, "2: beta") {
		t.Errorf("expected '2: beta' in output, got:\n%s", result.Content)
	}
	// Must NOT contain "2:beta" (missing space).
	badPattern := regexp.MustCompile(`\d:beta`)
	if badPattern.MatchString(result.Content) {
		t.Errorf("output has missing space after colon:\n%s", result.Content)
	}
}

// TestGrep_MultipleFiles verifies directory search still formats correctly.
func TestGrep_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package main\nfunc main() {}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte("package main\nfunc helper() {}"), 0644)

	tool := Grep{}
	args, _ := json.Marshal(map[string]any{
		"pattern": "func",
		"path":    tmpDir,
	})
	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Each match line (those starting with a digit) must use "N: content" format.
	lineNumStart := regexp.MustCompile(`^\d`)
	validFormat := regexp.MustCompile(`^\d+: `)
	for _, line := range strings.Split(result.Content, "\n") {
		if !lineNumStart.MatchString(line) {
			continue // skip separators, file paths, blank lines
		}
		if !validFormat.MatchString(line) {
			t.Errorf("match line %q does not match 'N: content' format", line)
		}
	}
}

// TestGrep_NoMatches verifies a clean "no matches" message when nothing is found.
func TestGrep_NoMatches(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "f.txt"), []byte("hello world"), 0644)

	tool := Grep{}
	args, _ := json.Marshal(map[string]any{
		"pattern": "zzznomatch",
		"path":    tmpDir,
	})
	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "No matches found") {
		t.Errorf("expected 'No matches found', got: %s", result.Content)
	}
}

// TestLs_LongFormat verifies the long listing includes size and modified-time columns.
func TestLs_LongFormat(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("hello"), 0644)

	tool := Ls{}
	args, _ := json.Marshal(map[string]any{
		"path": tmpDir,
		"long": true,
	})
	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Long format: "- <size> <datetime> readme.txt"
	if !strings.Contains(result.Content, "readme.txt") {
		t.Errorf("file name missing from long output:\n%s", result.Content)
	}
	// Should contain a numeric size field.
	sizeRe := regexp.MustCompile(`\d+\s+\d{4}-\d{2}-\d{2}`)
	if !sizeRe.MatchString(result.Content) {
		t.Errorf("long format missing size/date columns:\n%s", result.Content)
	}
}

// TestLs_LongFormat_Directory verifies directory entries are marked with "d".
func TestLs_LongFormat_Directory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	_ = os.Mkdir(subDir, 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)

	tool := Ls{}
	args, _ := json.Marshal(map[string]any{
		"path": tmpDir,
		"long": true,
	})
	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Directory entries must start with "d ".
	for _, line := range strings.Split(result.Content, "\n") {
		if strings.Contains(line, "subdir") && !strings.HasPrefix(line, "d ") {
			t.Errorf("directory entry should start with 'd': %q", line)
		}
		if strings.Contains(line, "file.txt") && !strings.HasPrefix(line, "- ") {
			t.Errorf("file entry should start with '-': %q", line)
		}
	}
}

// TestLs_ShortFormat verifies the default (no long) listing still works.
func TestLs_ShortFormat(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)

	tool := Ls{}
	args, _ := json.Marshal(map[string]any{"path": tmpDir})
	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "a.txt" {
		t.Errorf("expected 'a.txt', got %q", result.Content)
	}
}
