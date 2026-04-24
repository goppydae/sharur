package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Ls is a tool for listing directory contents.
type Ls struct{}

func (Ls) Name() string { return "ls" }

func (Ls) Description() string {
	return "List files and directories in a given path."
}

func (Ls) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to list. Defaults to current directory."
			},
			"recursive": {
				"type": "boolean",
				"description": "List recursively. Defaults to false."
			},
			"long": {
				"type": "boolean",
				"description": "Use long format showing size, modified time, and type. Defaults to false."
			}
		}
	}`)
}

func (Ls) Execute(ctx context.Context, args json.RawMessage, update ToolUpdate) (*ToolResult, error) {
	var params struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
		Long      bool   `json:"long"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	var entries []string
	if params.Recursive {
		if walkErr := filepath.Walk(absPath, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				log.Printf("ls: walk error at %s: %v", p, err)
				return nil
			}
			rel, _ := filepath.Rel(absPath, p)
			if rel == "." {
				return nil
			}
			if params.Long {
				entries = append(entries, formatLong(rel, info))
			} else {
				entries = append(entries, rel)
			}
			return nil
		}); walkErr != nil {
			return nil, fmt.Errorf("walk %s: %w", absPath, walkErr)
		}
	} else {
		dir, err := os.ReadDir(absPath)
		if err != nil {
			return nil, fmt.Errorf("read directory: %w", err)
		}
		for _, d := range dir {
			if params.Long {
				info, err := d.Info()
				if err != nil {
					entries = append(entries, d.Name())
					continue
				}
				entries = append(entries, formatLong(d.Name(), info))
			} else {
				entries = append(entries, d.Name())
			}
		}
	}

	result := &ToolResult{
		Content: strings.Join(entries, "\n"),
		Metadata: map[string]any{
			"path":  absPath,
			"count": len(entries),
		},
	}

	if update != nil {
		update(result)
	}

	return result, nil
}

// formatLong formats a single entry in long format: "<type> <size> <modtime> <name>".
func formatLong(name string, info os.FileInfo) string {
	kind := "-"
	if info.IsDir() {
		kind = "d"
	} else if info.Mode()&os.ModeSymlink != 0 {
		kind = "l"
	}
	return fmt.Sprintf("%s %8d %s %s", kind, info.Size(), info.ModTime().Format(time.DateTime), name)
}
