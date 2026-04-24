package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Find is a tool for finding files by glob pattern.
type Find struct{}

func (Find) Name() string { return "find" }

func (Find) Description() string {
	return "Find files matching a glob pattern. Supports recursive search and filtering."
}

func (Find) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Root directory to search in. Defaults to current directory."
			},
			"glob": {
				"type": "string",
				"description": "Glob pattern to match (e.g., '*.go', '**/*.txt'). Required."
			},
			"type": {
				"type": "string",
				"description": "Filter by type: 'f' for files, 'd' for directories. Defaults to 'f'."
			}
		},
		"required": ["glob"]
	}`)
}

func (Find) Execute(ctx context.Context, args json.RawMessage, update ToolUpdate) (*ToolResult, error) {
	var params struct {
		Path string `json:"path"`
		Glob string `json:"glob"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Glob == "" {
		return nil, fmt.Errorf("glob pattern is required")
	}

	root := params.Path
	if root == "" {
		root = "."
	}

	absRoot, err := filepath.Abs(NormalizePath(root))
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	var matches []string

	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}

	if !info.IsDir() {
		// Single file check
		 matched, err := filepath.Match(params.Glob, filepath.Base(absRoot))
		if err != nil {
			return nil, fmt.Errorf("match: %w", err)
		}
		if matched && shouldInclude(absRoot, params.Type) {
			matches = append(matches, absRoot)
		}
	} else {
		// Recursive find
		matches, err = findFiles(absRoot, params.Glob, params.Type)
		if err != nil {
			return nil, err
		}
	}

	if len(matches) == 0 {
		result := &ToolResult{
			Content: "No files found.",
			Metadata: map[string]any{
				"path":  absRoot,
				"count": 0,
			},
		}
		if update != nil {
			update(result)
		}
		return result, nil
	}

	result := &ToolResult{
		Content: strings.Join(matches, "\n"),
		Metadata: map[string]any{
			"path":  absRoot,
			"count": len(matches),
		},
	}

	if update != nil {
		update(result)
	}

	return result, nil
}

func shouldInclude(path, fileType string) bool {
	if fileType == "" || fileType == "f" {
		info, err := os.Stat(path)
		if err != nil {
			return true // Include by default
		}
		return !info.IsDir()
	}
	if fileType == "d" {
		info, err := os.Stat(path)
		if err != nil {
			return false
		}
		return info.IsDir()
	}
	return true
}

func findFiles(root, glob, fileType string) ([]string, error) {
	var matches []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip hidden directories and vendor
		if strings.HasPrefix(filepath.Base(path), ".") && info.IsDir() {
			if path != root {
				return filepath.SkipDir
			}
		}
		if strings.Contains(path, "/vendor/") || strings.Contains(path, "/.git/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Check type filter
		if !shouldInclude(path, fileType) {
			return nil
		}

		// Match glob
		matched, err := filepath.Match(glob, filepath.Base(path))
		if err != nil {
			return nil
		}
		if matched {
			matches = append(matches, path)
		}

		return nil
	})

	return matches, err
}

func (Find) IsReadOnly() bool { return true }
