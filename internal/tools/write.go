package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write is a tool for creating or overwriting files.
type Write struct{}

func (Write) Name() string { return "write" }

func (Write) Description() string {
	return "Create or overwrite a file with the given content. Creates parent directories if needed."
}

func (Write) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to the file to create/overwrite"
			},
			"content": {
				"type": "string",
				"description": "Content to write to the file"
			}
		},
		"required": ["path", "content"]
	}`)
}

func (Write) Execute(ctx context.Context, args json.RawMessage, update ToolUpdate) (*ToolResult, error) {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	if params.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	// Resolve path
	path, err := filepath.Abs(NormalizePath(params.Path))
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create directories: %w", err)
	}

	// Write file
	if err := os.WriteFile(path, []byte(params.Content), 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	result := &ToolResult{
		Content: fmt.Sprintf("Written %d bytes to %s", len(params.Content), path),
		Metadata: map[string]any{
			"path":   path,
			"size":   info.Size(),
			"mode":   info.Mode().String(),
		},
	}

	if update != nil {
		update(result)
	}

	return result, nil
}

func (Write) IsReadOnly() bool { return false }
