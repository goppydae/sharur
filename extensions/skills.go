package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/skills"
	"github.com/goppydae/gollm/internal/tools"
)

// SkillsMetadataExtension lists all available skills in the system prompt.
type SkillsMetadataExtension struct {
	agent.NoopExtension
	skills []*skills.Skill
}

// NewSkillsMetadataExtension creates an extension that adds skill metadata to the prompt.
func NewSkillsMetadataExtension(allSkills []*skills.Skill) *SkillsMetadataExtension {
	return &SkillsMetadataExtension{
		NoopExtension: agent.NoopExtension{NameStr: "skills-metadata"},
		skills:        allSkills,
	}
}

// Tools returns a SkillTool for each loaded skill.
func (s *SkillsMetadataExtension) Tools() []tools.Tool {
	var skillTools []tools.Tool
	for _, sk := range s.skills {
		skillTools = append(skillTools, &SkillTool{skill: sk})
	}
	return skillTools
}

// ModifySystemPrompt injects a brief list of skills into the system prompt.
// This tells the agent it can call these skills (as tools) when needed.
func (s *SkillsMetadataExtension) ModifySystemPrompt(prompt string) string {
	if len(s.skills) == 0 {
		return prompt
	}

	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\n\nThe following skills are available to you as specialized instruction sets. ")
	sb.WriteString("You can call them like tools (prefixed with 'skill_') to load their specialized instructions into your context.\n\n")
	sb.WriteString("<available_skills>\n")
	for _, sk := range s.skills {
		desc := sk.Description
		if desc == "" {
			desc = "Specialized instructions for " + sk.Name
		}
		fmt.Fprintf(&sb, "- skill_%s: %s\n", sk.Name, desc)
	}
	sb.WriteString("</available_skills>\n")

	return sb.String()
}

// SkillTool implements tools.Tool for a single Markdown-based skill.
type SkillTool struct {
	skill *skills.Skill
}

func (s *SkillTool) Name() string { return "skill_" + s.skill.Name }
func (s *SkillTool) Description() string {
	d := s.skill.Description
	if d == "" {
		d = "Loads specialized instructions for " + s.skill.Name
	}
	return d
}

func (s *SkillTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"args": {
				"type": "string",
				"description": "Optional arguments or context to resolve against the skill"
			}
		}
	}`)
}

func (s *SkillTool) Execute(ctx context.Context, args json.RawMessage, update tools.ToolUpdate) (*tools.ToolResult, error) {
	var params struct {
		Args string `json:"args"`
	}
	_ = json.Unmarshal(args, &params)

	content := s.skill.Content
	if params.Args != "" {
		content += "\n\nArguments provided for resolution:\n" + params.Args
	}

	return &tools.ToolResult{
		Content: content,
	}, nil
}

// SkillLoader discovers and loads Markdown-based skills.
type SkillLoader struct {
	Dirs []string
}

// NewSkillLoader creates a new loader for Markdown skills.
func NewSkillLoader(dirs []string) *SkillLoader {
	return &SkillLoader{Dirs: dirs}
}

// Load finds all skills and returns a SkillsMetadataExtension.
func (l *SkillLoader) Load() ([]agent.Extension, error) {
	all, err := skills.Discover(l.Dirs...)
	if err != nil {
		return nil, err
	}

	if len(all) == 0 {
		return nil, nil
	}

	return []agent.Extension{NewSkillsMetadataExtension(all)}, nil
}
