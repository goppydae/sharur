package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	// Create temp file
	tmp, err := os.CreateTemp("", "test-read-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	content := "line1\nline2\nline3\n"
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = tmp.Close()

	// Test read
	var tool Read
	args, _ := json.Marshal(map[string]any{
		"path":   tmp.Name(),
		"offset": 1,
		"limit":  2,
	})

	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Content, "line1") {
		t.Errorf("expected 'line1' in output, got: %s", result.Content)
	}

	// Test read missing file
	args, _ = json.Marshal(map[string]any{"path": "non-existent.txt"})
	result, err = tool.Execute(context.Background(), args, nil)
	if err == nil && !result.IsError {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestWrite(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	var tool Write
	args, _ := json.Marshal(map[string]any{
		"path":    path,
		"content": "Hello, World!",
	})

	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}

	// Verify file was written
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got: %s", string(data))
	}
}

func TestEdit(t *testing.T) {
	// Create temp file
	tmp, err := os.CreateTemp("", "test-edit-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	content := "Hello, World!\nHello, Go!"
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = tmp.Close()

	var tool Edit
	args, _ := json.Marshal(map[string]any{
		"path":    tmp.Name(),
		"oldText": "World",
		"newText": "Universe",
	})

	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}

	// Verify edit
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	expected := "Hello, Universe!\nHello, Go!"
	if string(data) != expected {
		t.Errorf("expected '%s', got: %s", expected, string(data))
	}
}

func TestBash(t *testing.T) {
	var tool Bash
	args, _ := json.Marshal(map[string]any{
		"command": "echo hello",
	})

	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Content, "hello") {
		t.Errorf("expected 'hello' in output, got: %s", result.Content)
	}
}

func TestLs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some files
	_ = os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)

	var tool Ls
	args, _ := json.Marshal(map[string]any{
		"path": tmpDir,
	})

	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Content, "a.txt") {
		t.Errorf("expected 'a.txt' in output, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "b.txt") {
		t.Errorf("expected 'b.txt' in output, got: %s", result.Content)
	}

	// Test ls missing dir
	args, _ = json.Marshal(map[string]any{"path": filepath.Join(tmpDir, "missing")})
	result, err = tool.Execute(context.Background(), args, nil)
	if err == nil && !result.IsError {
		t.Error("expected error for missing directory, got nil")
	}
}

func TestGrep(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	_ = os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("func main() {\n\thello()\n}"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte("func hello() {\n\tfmt.Println(\"hi\")\n}"), 0644)

	var tool Grep
	args, _ := json.Marshal(map[string]any{
		"pattern": "func",
		"path":    tmpDir,
	})

	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result.Content, "No matches found") {
		t.Errorf("expected matches, got: %s", result.Content)
	}
}

func TestFind(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	_ = os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("a"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte("b"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "c.txt"), []byte("c"), 0644)

	var tool Find
	args, _ := json.Marshal(map[string]any{
		"glob": "*.go",
		"path": tmpDir,
	})

	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result.Content, "No files found") {
		t.Errorf("expected .go files, got: %s", result.Content)
	}
	if strings.Contains(result.Content, "c.txt") {
		t.Errorf("expected no .txt files, got: %s", result.Content)
	}

	// Test find missing path
	args, _ = json.Marshal(map[string]any{"path": "/non/existent/path"})
	result, err = tool.Execute(context.Background(), args, nil)
	if err == nil && !result.IsError {
		t.Error("expected error for missing path, got nil")
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"README.md", "README.md"},
		{"@README.md", "README.md"},
		{"/path/to/file", "/path/to/file"},
		{"@/path/to/file", "/path/to/file"},
		{"", ""},
		{"@", ""},
	}

	for _, tt := range tests {
		if got := NormalizePath(tt.input); got != tt.expected {
			t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
