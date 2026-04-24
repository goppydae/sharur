package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Edit is a tool for performing search-replace edits on files.
type Edit struct{}

func (Edit) Name() string { return "edit" }

func (Edit) Description() string {
	return "Perform a search-replace edit on a file. Supports regex patterns and multiple edits."
}

func (Edit) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to the file to edit"
			},
			"oldText": {
				"type": "string",
				"description": "Text to find (supports regex if regex=true)"
			},
			"newText": {
				"type": "string",
				"description": "Text to replace with"
			},
			"regex": {
				"type": "boolean",
				"description": "Treat oldText as regex. Defaults to false."
			},
			"offset": {
				"type": "integer",
				"description": "Line number to start searching from (1-indexed). Defaults to 1."
			}
		},
		"required": ["path", "oldText", "newText"]
	}`)
}

func (Edit) Execute(ctx context.Context, args json.RawMessage, update ToolUpdate) (*ToolResult, error) {
	var params struct {
		Path   string `json:"path"`
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
		Regex  bool   `json:"regex"`
		Offset int    `json:"offset"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if params.OldText == "" {
		return nil, fmt.Errorf("oldText is required")
	}

	// Read file
	absPath, err := filepath.Abs(params.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Perform edit
	var result string
	if params.Regex {
		result, err = editRegex(string(data), params.OldText, params.NewText, params.Offset)
	} else {
		result, err = editLiteral(string(data), params.OldText, params.NewText, params.Offset)
	}
	if err != nil {
		return nil, err
	}

	// Write modified content
	if err := os.WriteFile(absPath, []byte(result), 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	editResult := &ToolResult{
		Content: fmt.Sprintf("Successfully edited %s (new size: %d bytes)", absPath, info.Size()),
		Metadata: map[string]any{
			"path": absPath,
			"size": info.Size(),
		},
	}

	if update != nil {
		update(editResult)
	}

	return editResult, nil
}

func editLiteral(content, oldText, newText string, offset int) (string, error) {
	if offset > 0 {
		lines := strings.Split(content, "\n")
		start := offset - 1
		if start > len(lines) {
			start = len(lines)
		}
		prefix := strings.Join(lines[:start], "\n")
		suffix := strings.Join(lines[start:], "\n")
		if !strings.Contains(suffix, oldText) {
			return "", fmt.Errorf("text not found: %q", oldText)
		}
		return prefix + "\n" + strings.Replace(suffix, oldText, newText, 1), nil
	}

	if !strings.Contains(content, oldText) {
		return "", fmt.Errorf("text not found: %q", oldText)
	}

	return strings.Replace(content, oldText, newText, 1), nil
}

func editRegex(content, pattern, replacement string, offset int) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	if offset > 0 {
		lines := strings.Split(content, "\n")
		start := offset - 1
		if start > len(lines) {
			start = len(lines)
		}
		prefix := strings.Join(lines[:start], "\n")
		suffix := strings.Join(lines[start:], "\n")
		if !re.MatchString(suffix) {
			return "", fmt.Errorf("regex pattern not found: %q", pattern)
		}
		return prefix + "\n" + re.ReplaceAllString(suffix, replacement), nil
	}

	if !re.MatchString(content) {
		return "", fmt.Errorf("regex pattern not found: %q", pattern)
	}

	return re.ReplaceAllString(content, replacement), nil
}
