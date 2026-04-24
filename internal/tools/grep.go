package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Grep is a tool for searching file contents with regex.
type Grep struct{}

func (Grep) Name() string { return "grep" }

func (Grep) Description() string {
	return "Search file contents for a regex pattern. Supports glob patterns and context lines."
}

func (Grep) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Regex pattern to search for"
			},
			"path": {
				"type": "string",
				"description": "File or directory to search in. Defaults to current directory."
			},
			"glob": {
				"type": "string",
				"description": "Glob pattern to filter files (e.g., '*.go')"
			},
			"before": {
				"type": "integer",
				"description": "Number of context lines before match. Defaults to 0."
			},
			"after": {
				"type": "integer",
				"description": "Number of context lines after match. Defaults to 0."
			},
			"maxMatches": {
				"type": "integer",
				"description": "Maximum number of matches to return. Defaults to 100."
			}
		},
		"required": ["pattern"]
	}`)
}

func (Grep) Execute(ctx context.Context, args json.RawMessage, update ToolUpdate) (*ToolResult, error) {
	var params struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		Before     int    `json:"before"`
		After      int    `json:"after"`
		MaxMatches int    `json:"maxMatches"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if params.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	absPath, err := filepath.Abs(NormalizePath(path))
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	maxMatches := params.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 100
	}

	matches, err := searchGrep(absPath, params.Pattern, params.Glob, params.Before, params.After, maxMatches)
	if err != nil {
		return nil, err
	}

	result := &ToolResult{
		Content: matches,
		Metadata: map[string]any{
			"path":       absPath,
			"matchCount": countMatches(matches),
		},
	}

	if update != nil {
		update(result)
	}

	return result, nil
}

func countMatches(s string) int {
	if s == "" {
		return 0
	}
	count := 0
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		if line != "" && !strings.HasPrefix(line, "--") && !strings.HasPrefix(line, "======") {
			count++
		}
	}
	return count
}

func searchGrep(root, pattern, glob string, before, after, maxMatches int) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	var results strings.Builder
	matchCount := 0

	// Determine if root is a file or directory
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat: %w", err)
	}

	if !info.IsDir() {
		// Single file search
		found, _, err := grepFile(root, re, before, after, maxMatches)
		if err != nil {
			return "", err
		}
		return found, nil
	}

	// Directory search
	var files []string
	if glob != "" {
		patterns, err := filepath.Glob(filepath.Join(root, "**", glob))
		if err != nil {
			return "", fmt.Errorf("glob: %w", err)
		}
		files = patterns
	} else {
		// Walk directory
		if walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Printf("grep: walk error at %s: %v", path, err)
				return nil
			}
			if info.IsDir() {
				return nil
			}
			// Skip vendor and VCS directories
			if strings.Contains(path, "/vendor/") || strings.Contains(path, "/.git/") {
				return nil
			}
			files = append(files, path)
			return nil
		}); walkErr != nil {
			return "", fmt.Errorf("walk %s: %w", root, walkErr)
		}
	}

	for _, file := range files {
		if matchCount >= maxMatches {
			break
		}
		found, count, err := grepFile(file, re, before, after, maxMatches-matchCount)
		if err != nil {
			continue
		}
		if found != "" {
			if results.Len() > 0 {
				results.WriteString("\n")
			}
			fmt.Fprintf(&results, "======\n%s\n======\n", file)
			results.WriteString(found)
			matchCount += count
		}
	}

	if results.Len() == 0 {
		return "No matches found.", nil
	}

	return results.String(), nil
}

func grepFile(file string, re *regexp.Regexp, before, after, maxMatches int) (string, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	var matches []struct {
		lineNum int
		content string
	}

	scanner := bufio.NewScanner(f)
	totalLines := 0
	for scanner.Scan() {
		totalLines++
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, struct {
				lineNum int
				content string
			}{totalLines, line})
		}
		if len(matches) >= maxMatches {
			break
		}
	}

	if len(matches) == 0 {
		return "", 0, nil
	}

	var results strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&results, "%d: %s\n", m.lineNum, m.content)
	}

	return results.String(), len(matches), nil
}

func (Grep) IsReadOnly() bool { return true }
