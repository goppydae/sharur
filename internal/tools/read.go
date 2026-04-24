package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Read is a tool for reading file contents.
type Read struct{}

func (Read) Name() string { return "read" }

func (Read) Description() string {
	return "Read a file from the filesystem. Supports offset and limit for large files."
}

func (Read) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to the file to read"
			},
			"offset": {
				"type": "integer",
				"description": "Line number to start reading from (1-indexed). Defaults to 1."
			},
			"limit": {
				"type": "integer",
				"description": "Maximum number of lines to read. Defaults to 200."
			}
		},
		"required": ["path"]
	}`)
}

func (Read) Execute(ctx context.Context, args json.RawMessage, update ToolUpdate) (*ToolResult, error) {
	var params struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	absPath, err := filepath.Abs(NormalizePath(path))
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	// Read file
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	content := string(data)

	// Apply offset and limit
	lines := splitLines(content)
	if params.Offset > 0 {
		start := params.Offset - 1
		if start > len(lines) {
			start = len(lines)
		}
		lines = lines[start:]
	}

	if params.Limit > 0 && params.Limit < len(lines) {
		lines = lines[:params.Limit]
	}

	output := stringsJoined(lines, "\n")
	if update != nil {
		update(&ToolResult{Content: output})
	}

	return &ToolResult{
		Content: output,
		Metadata: map[string]any{
			"path":   absPath,
			"lines":  len(lines),
			"size":   len(data),
		},
	}, nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func stringsJoined(s []string, sep string) string {
	return strings.Join(s, sep)
}

func (Read) IsReadOnly() bool { return true }
