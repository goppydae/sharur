// Package prompts provides discovery, parsing, and expansion of prompt template files.
package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goppydae/gollm/internal/config"
)

// Prompt represents a parsed prompt template.
type Prompt struct {
	Description  string
	ArgumentHint string
	Template     string
	Path         string
}

// DefaultDirs returns the standard prompt search directories.
func DefaultDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".gollm", "prompts"),
	}
	if projectGollm := config.FindProjectGollm(); projectGollm != "" {
		dirs = append(dirs, filepath.Join(projectGollm, "prompts"))
	}
	dirs = append(dirs, ".gollm/prompts") // fallback
	return dirs
}

// Discover finds and loads all .md prompt files in the given directories.
// It recurses into subdirectories.
func Discover(dirs ...string) ([]*Prompt, error) {
	var out []*Prompt
	for _, root := range dirs {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() || strings.ToLower(filepath.Ext(d.Name())) != ".md" {
				return nil
			}
			p, err := Load(path)
			if err != nil {
				return nil
			}
			out = append(out, p)
			return nil
		})
	}
	return out, nil
}

// Load reads and parses a prompt template file.
func Load(path string) (*Prompt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(string(data), path), nil
}

// Expand substitutes positional arguments ($1, $2, …) into the template.
// It wraps arguments in <untrusted_input> tags to mitigate prompt injection.
// Missing arguments are substituted with an empty tagged block so that no
// literal "$N" placeholder ever reaches the model.
func Expand(p *Prompt, args ...string) string {
	result := p.Template

	// Count how many $N placeholders exist in the template so we substitute all of them.
	maxN := 0
	for i := 1; strings.Contains(result, fmt.Sprintf("$%d", i)); i++ {
		maxN = i
	}

	for i := 1; i <= maxN; i++ {
		var raw string
		if i <= len(args) {
			raw = args[i-1]
		}
		// Basic sanitization: prevent breakout from the tag
		raw = strings.ReplaceAll(raw, "</untrusted_input>", "[REDACTED]")
		tagged := fmt.Sprintf("<untrusted_input>\n%s\n</untrusted_input>", raw)
		result = strings.ReplaceAll(result, fmt.Sprintf("$%d", i), tagged)
	}
	return strings.TrimSpace(result)
}

func parse(content, path string) *Prompt {
	p := &Prompt{Path: path}

	if !strings.HasPrefix(content, "---") {
		p.Template = strings.TrimSpace(content)
		return p
	}

	lines := strings.Split(content, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		p.Template = strings.TrimSpace(content)
		return p
	}

	for _, line := range lines[1:end] {
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		switch k {
		case "description":
			p.Description = v
		case "argument-hint":
			p.ArgumentHint = v
		}
	}

	p.Template = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return p
}
