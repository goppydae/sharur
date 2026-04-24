// Package skills provides discovery and parsing of SKILL.md files.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goppydae/gollm/internal/config"
)

// Skill represents a parsed skill definition.
type Skill struct {
	Name        string
	Description string
	Content     string // markdown body after frontmatter
	Path        string
}

// DefaultDirs returns the standard skill search directories.
func DefaultDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".gollm", "skills"),
	}
	if projectGollm := config.FindProjectGollm(); projectGollm != "" {
		dirs = append(dirs, filepath.Join(projectGollm, "skills"))
	}
	dirs = append(dirs, ".gollm/skills") // fallback
	return dirs
}

// Discover finds and loads all .md skill files in the given directories.
// It recurses into subdirectories. If a directory contains SKILL.md, it is
// treated as a skill root and its subdirectories are not scanned.
func Discover(dirs ...string) ([]*Skill, error) {
	var skills []*Skill
	seen := map[string]bool{}

	for _, root := range dirs {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			
			// If it's a file, check if it's a skill
			if !d.IsDir() {
				if strings.ToLower(filepath.Ext(d.Name())) == ".md" {
					s, err := Load(path)
					if err == nil && !seen[s.Name] {
						seen[s.Name] = true
						skills = append(skills, s)
					}
				}
				return nil
			}

			// Check for SKILL.md in this directory
			skillPath := filepath.Join(path, "SKILL.md")
			if _, err := os.Stat(skillPath); err == nil {
				s, err := Load(skillPath)
				if err == nil && !seen[s.Name] {
					seen[s.Name] = true
					skills = append(skills, s)
				}
				return filepath.SkipDir // Don't recurse further if SKILL.md is found
			}

			return nil
		})
	}
	return skills, nil
}

// Load reads and parses a skill file.
func Load(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(string(data), path), nil
}

// parse extracts YAML frontmatter and body from a skill document.
func parse(content, path string) *Skill {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if strings.ToUpper(filepath.Base(path)) == "SKILL.MD" {
		name = filepath.Base(filepath.Dir(path))
	}
	s := &Skill{
		Path: path,
		Name: name,
	}

	if !strings.HasPrefix(content, "---") {
		s.Content = content
		return s
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
		s.Content = content
		return s
	}

	for _, line := range lines[1:end] {
		k, v, ok := splitKV(line)
		if !ok {
			continue
		}
		switch k {
		case "name":
			s.Name = v
		case "description":
			s.Description = v
		}
	}

	s.Content = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return s
}

func splitKV(line string) (key, val string, ok bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

// FormatSkillsForPrompt generates a system prompt section listing available skills.
func FormatSkillsForPrompt(skills []*Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\nThe following skills provide specialized instructions for specific tasks.\n")
	sb.WriteString("Use the read tool to load a skill's file when the task matches its description.\n")
	sb.WriteString("When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.\n\n")
	sb.WriteString("<available_skills>\n")

	for _, s := range skills {
		desc := s.Description
		if desc == "" {
			desc = "Specialized instructions for " + s.Name
		}
		fmt.Fprintf(&sb, "- %s: %s (path: %s)\n", s.Name, desc, s.Path)
	}

	sb.WriteString("</available_skills>\n")
	return sb.String()
}
