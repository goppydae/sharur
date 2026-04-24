package interactive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/config"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/prompts"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/skills"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// slashCommand represents a parsed slash command.
type slashCommand struct {
	name string
	arg  string // remainder after command name
	raw  string // full command string (including /)
}

// parseSlashCommand parses a leading /command [args] from text.
// Returns nil if the text doesn't start with a slash command.
func parseSlashCommand(text string) *slashCommand {
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	// Find the command name (up to first space or end of string)
	rest := text[1:] // strip leading /
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		return &slashCommand{name: rest, arg: "", raw: text}
	}
	return &slashCommand{
		name: rest[:spaceIdx],
		arg:  strings.TrimSpace(rest[spaceIdx+1:]),
		raw:  text,
	}
}


// AvailableSlashCommands lists all known slash commands for autocomplete.
// BaseSlashCommands lists the static slash commands.
var BaseSlashCommands = []string{"new", "resume", "fork", "clone", "tree", "import", "export", "model", "stats", "compact", "config", "context", "exit", "quit"}

// knownCommand returns true if name is a recognized slash command.
func knownCommand(name string) bool {
	if strings.HasPrefix(name, "skill:") || strings.HasPrefix(name, "prompt:") {
		return true
	}
	for _, c := range BaseSlashCommands {
		if c == name {
			return true
		}
	}
	return false
}

// slashResult holds the result of processing a slash command.
type slashResult struct {
	historyEntry historyEntry // appended to history as a system message
	modalKind    modalKind    // opens a modal overlay
	modalNodes   []session.FlatNode
	compact      bool         // if true, triggers context compaction
	syncHistory  bool         // if true, triggers TUI history sync from agent
	quit         bool         // if true, triggers tea.Quit
	expandInput  string       // if non-empty, replaces the editor input with this text
	sendDirectly string       // if non-empty, sends this text to the agent immediately
	invokeTool     string     // if non-empty, manually triggers a tool call
	invokeToolArgs string     // arguments for the manual tool call
}

// handleSlashCommand processes a slash command and returns the result.
func handleSlashCommand(cmd *slashCommand, mgr *session.Manager, ag *agent.Agent, cfg *config.Config) (*slashResult, error) {
	switch {
	case cmd.name == "new":
		return handleNewSession(mgr, ag)

	case cmd.name == "resume":
		return handleResumeSession(mgr, ag, cmd.arg)

	case cmd.name == "fork":
		return handleForkSession(mgr, ag)

	case cmd.name == "clone":
		return handleCloneSession(mgr, ag)

	case cmd.name == "tree":
		return handleTreeCommand(mgr, ag)

	case cmd.name == "import":
		return handleImportSession(mgr, ag, cmd.arg)

	case cmd.name == "export":
		return handleExportSession(mgr, ag, cmd.arg)

	case cmd.name == "model":
		return handleModelCommand(ag, cfg, cmd.arg)

	case cmd.name == "stats":
		return handleStatsCommand(ag)

	case cmd.name == "compact":
		return handleCompact(ag)

	case cmd.name == "config":
		return handleConfigCommand(cfg)

	case cmd.name == "context":
		return handleContextCommand(ag)

	case cmd.name == "skill":
		return handleSkillCommand(ag, cfg, cmd.arg, "") // /skill command itself usually doesn't have args after it, but we can support it

	case cmd.name == "prompt":
		return handlePromptCommand(cfg, cmd.arg)

	case strings.HasPrefix(cmd.name, "skill:"):
		skillName := strings.TrimPrefix(cmd.name, "skill:")
		return handleSkillCommand(ag, cfg, skillName, cmd.arg)

	case strings.HasPrefix(cmd.name, "prompt:"):
		promptName := strings.TrimPrefix(cmd.name, "prompt:")
		return handlePromptCommand(cfg, promptName)

	case cmd.name == "exit" || cmd.name == "quit":
		return &slashResult{quit: true}, nil

	default:
		// Check for direct skill or prompt
		if res, err := handleSkillCommand(ag, cfg, cmd.name, cmd.arg); err == nil && res.sendDirectly != "" {
			return res, nil
		}
		if res, err := handlePromptCommand(cfg, cmd.name); err == nil && res.expandInput != "" {
			return res, nil
		}

		return &slashResult{
			historyEntry: historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Unknown command: /%s", cmd.name)}}},
		}, nil
	}
}

// handleNewSession creates a new session via the manager.
func handleNewSession(mgr *session.Manager, ag *agent.Agent) (*slashResult, error) {
	sess, err := mgr.Create()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	ag.LoadSession(sess.ToTypes())
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("New session created: %s", sess.ID)}}},
		syncHistory:  true,
	}, nil
}

// handleResumeSession loads a session by ID or path.
func handleResumeSession(mgr *session.Manager, ag *agent.Agent, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /resume <session-id-or-path>"}}},
		}, nil
	}

	sess, err := mgr.Load(arg)
	if err != nil {
		// Try as a file path
		if abs, err2 := filepath.Abs(arg); err2 == nil {
			sess, err = mgr.Load(abs)
		}
		if err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}
	}

	ag.LoadSession(sess.ToTypes())
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Resumed session: %s (%d messages)", sess.ID, len(sess.Messages))}}},
		syncHistory:  true,
	}, nil
}

// handleImportSession imports a session from a JSONL file.
func handleImportSession(mgr *session.Manager, ag *agent.Agent, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /import <path-to-jsonl>"}}},
		}, nil
	}

	// Try loading from path first if it looks like a path or the file exists
	var sess *session.Session
	var err error
	if strings.Contains(arg, string(os.PathSeparator)) || strings.HasSuffix(arg, ".jsonl") {
		sess, err = mgr.LoadPath(arg)
	} else {
		sess, err = mgr.Load(arg)
	}

	if err != nil {
		return nil, fmt.Errorf("import session: %w", err)
	}

	// Create a new session that imports the messages
	newSess, err := mgr.Create()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	newSess.Messages = sess.Messages
	newSess.Model = sess.Model
	newSess.Provider = sess.Provider
	newSess.Thinking = sess.Thinking
	newSess.SystemPrompt = sess.SystemPrompt
	newSess.Name = "Imported: " + sess.Name
	if err := mgr.Save(newSess); err != nil {
		return nil, fmt.Errorf("save imported session: %w", err)
	}

	// Convert session.Session to types.Session for LoadSession
	typesSess := &types.Session{
		ID:           newSess.ID,
		ParentID:     newSess.ParentID,
		Name:         newSess.Name,
		CreatedAt:    newSess.CreatedAt,
		UpdatedAt:    newSess.UpdatedAt,
		Model:        newSess.Model,
		Provider:     newSess.Provider,
		Thinking:     types.ThinkingLevel(newSess.Thinking),
		SystemPrompt: newSess.SystemPrompt,
		Messages:     newSess.Messages,
	}
	ag.LoadSession(typesSess)
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Imported session: %s (%d messages)", newSess.ID, len(newSess.Messages))}}},
		syncHistory:  true,
	}, nil
}

// handleExportSession exports the current session to a JSONL file.
func handleExportSession(mgr *session.Manager, ag *agent.Agent, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /export <path-to-destination.jsonl>"}}},
		}, nil
	}

	// Get the session state from the agent and convert to session.Session
	agentSess := ag.GetSession()
	sessToSave := &session.Session{
		ID:           agentSess.ID,
		ParentID:     agentSess.ParentID,
		Name:         agentSess.Name,
		CreatedAt:    agentSess.CreatedAt,
		UpdatedAt:    agentSess.UpdatedAt,
		Model:        agentSess.Model,
		Provider:     agentSess.Provider,
		Thinking:     string(agentSess.Thinking),
		SystemPrompt: agentSess.SystemPrompt,
		Messages:     agentSess.Messages,
	}

	if err := mgr.SavePath(sessToSave, arg); err != nil {
		return nil, fmt.Errorf("export session: %w", err)
	}

	return &slashResult{
		historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Exported session to: %s", arg)}}},
	}, nil
}

// handleStatsCommand returns formatted session stats.
func handleStatsCommand(ag *agent.Agent) (*slashResult, error) {
	return &slashResult{
		modalKind: modalStats,
	}, nil
}



// handleCompact triggers compaction on the agent.
func handleCompact(ag *agent.Agent) (*slashResult, error) {
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: "Compacting session context..."}}},
		compact:      true,
	}, nil
}

// handleConfigCommand returns formatted config info.
func handleConfigCommand(cfg *config.Config) (*slashResult, error) {
	home, _ := os.UserHomeDir()
	anthropicKeySet := cfg.AnthropicAPIKey != ""

	var sb strings.Builder
	sb.WriteString("Configuration\n")
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "Model: %s\n", cfg.Model)
	fmt.Fprintf(&sb, "Provider: %s\n", cfg.Provider)
	fmt.Fprintf(&sb, "Thinking: %s\n", cfg.ThinkingLevel)
	fmt.Fprintf(&sb, "Theme: %s\n", cfg.Theme)
	fmt.Fprintf(&sb, "Mode: %s\n", cfg.Mode)
	sb.WriteString("\n")
	sb.WriteString("Providers\n")
	fmt.Fprintf(&sb, "Ollama: %s\n", cfg.OllamaBaseURL)
	fmt.Fprintf(&sb, "OpenAI: %s\n", cfg.OpenAIBaseURL)
	if anthropicKeySet {
		sb.WriteString("Anthropic: key set\n")
	} else {
		sb.WriteString("Anthropic: (no key)\n")
	}
	fmt.Fprintf(&sb, "llama.cpp: %s\n", cfg.LlamaCppBaseURL)
	sb.WriteString("\n")
	sb.WriteString("Compaction\n")
	fmt.Fprintf(&sb, "Enabled: %v\n", cfg.Compaction.Enabled)
	fmt.Fprintf(&sb, "Reserve Tokens: %d\n", cfg.Compaction.ReserveTokens)
	fmt.Fprintf(&sb, "Keep Recent Tokens: %d\n", cfg.Compaction.KeepRecentTokens)
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "Session dir: %s\n", cfg.SessionDir)
	fmt.Fprintf(&sb, "Config: %s/.gollm/config.json\n", home)

	return &slashResult{
		modalKind:    modalConfig,
	}, nil
}

// handleContextCommand returns context usage info.
func handleContextCommand(ag *agent.Agent) (*slashResult, error) {
	stats := ag.GetStats()
	usageTxt := fmt.Sprintf("Context usage: %d tokens", stats.ContextTokens)
	if stats.ContextWindow > 0 {
		usageTxt = fmt.Sprintf("Context usage: %d / %d tokens (%.1f%%)", stats.ContextTokens, stats.ContextWindow, float64(stats.ContextTokens)/float64(stats.ContextWindow)*100)
	}
	usageTxt += fmt.Sprintf(" — %d messages, %d tool results, %d tool calls", stats.UserMessages+stats.AssistantMsgs, stats.ToolResults, stats.ToolCalls)

	return &slashResult{
		historyEntry: historyEntry{
			role: "info",
			items: []contentItem{{
				kind: contentItemText,
				text: usageTxt,
			}},
		},
	}, nil
}

// handleTreeCommand builds the session tree and opens the tree modal.
func handleTreeCommand(mgr *session.Manager, ag *agent.Agent) (*slashResult, error) {
	roots, err := mgr.BuildTree()
	if err != nil {
		return nil, err
	}
	nodes := session.FlattenTree(roots)

	return &slashResult{
		modalKind:  modalTree,
		modalNodes: nodes,
	}, nil
}

// handleForkSession creates a new session branched from the current one.
func handleForkSession(mgr *session.Manager, ag *agent.Agent) (*slashResult, error) {
	agentSess := ag.GetSession()
	source := &session.Session{
		ID:           agentSess.ID,
		ParentID:     agentSess.ParentID,
		Name:         agentSess.Name,
		Model:        agentSess.Model,
		Provider:     agentSess.Provider,
		Thinking:     string(agentSess.Thinking),
		SystemPrompt: agentSess.SystemPrompt,
		Messages:     agentSess.Messages,
	}
	forked, err := mgr.Fork(source)
	if err != nil {
		return nil, fmt.Errorf("fork session: %w", err)
	}
	ag.ResetSession(forked.ID)
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Forked into new session: %s (parent: %s)", forked.ID, agentSess.ID)}}},
		syncHistory:  true,
	}, nil
}

// handleCloneSession duplicates the current session into a new independent session.
func handleCloneSession(mgr *session.Manager, ag *agent.Agent) (*slashResult, error) {
	agentSess := ag.GetSession()
	source := &session.Session{
		ID:           agentSess.ID,
		Name:         agentSess.Name,
		Model:        agentSess.Model,
		Provider:     agentSess.Provider,
		Thinking:     string(agentSess.Thinking),
		SystemPrompt: agentSess.SystemPrompt,
		Messages:     agentSess.Messages,
	}
	cloned, err := mgr.Clone(source)
	if err != nil {
		return nil, fmt.Errorf("clone session: %w", err)
	}
	ag.ResetSession(cloned.ID)
	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Cloned into new session: %s", cloned.ID)}}},
		syncHistory:  true,
	}, nil
}

// handleModelCommand switches the agent's model mid-conversation.
// Accepts "provider/model" or just "model" format.
func handleModelCommand(ag *agent.Agent, cfg *config.Config, arg string) (*slashResult, error) {
	if arg == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /model <provider/model> (e.g. /model anthropic/claude-opus-4-5)"}}},
		}, nil
	}

	provider := cfg.Provider
	model := arg
	if idx := strings.IndexByte(arg, '/'); idx >= 0 {
		provider = arg[:idx]
		model = arg[idx+1:]
	}

	newProv := buildProviderFromNameAndModel(provider, model, cfg)
	ag.SetProvider(newProv)
	ag.SetModel(model)

	return &slashResult{
		historyEntry: historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Switched to %s/%s", provider, model)}}},
	}, nil
}

func handleSkillCommand(ag *agent.Agent, cfg *config.Config, skillName, args string) (*slashResult, error) {
	if skillName == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /skill:<name> [args]"}}},
		}, nil
	}

	skillDirs := append([]string{}, cfg.Extensions...)
	skillDirs = append(skillDirs, skills.DefaultDirs()...)
	skillDirs = append(skillDirs, cfg.SkillPaths...)

	allSkills, err := skills.Discover(skillDirs...)
	if err != nil {
		return nil, fmt.Errorf("failed to discover skills: %w", err)
	}

	var skill *skills.Skill
	for _, s := range allSkills {
		if s.Name == skillName {
			skill = s
			break
		}
	}

	if skill == nil {
		return nil, fmt.Errorf("skill %q not found", skillName)
	}

	// Return a tool invocation result
	argBytes, _ := json.Marshal(args)
	return &slashResult{
		invokeTool:     "skill_" + skill.Name,
		invokeToolArgs: fmt.Sprintf(`{"args":%s}`, string(argBytes)),
	}, nil
}

// handlePromptCommand expands a named prompt template into the editor input.
func handlePromptCommand(cfg *config.Config, promptName string) (*slashResult, error) {
	if promptName == "" {
		return &slashResult{
			historyEntry: historyEntry{role: "system", items: []contentItem{{kind: contentItemText, text: "Usage: /prompt:<name>"}}},
		}, nil
	}

	promptDirs := append([]string{}, prompts.DefaultDirs()...)
	promptDirs = append(promptDirs, cfg.PromptTemplatePaths...)

	allPrompts, err := prompts.Discover(promptDirs...)
	if err != nil {
		return nil, fmt.Errorf("failed to discover prompt templates: %w", err)
	}

	var content string
	for _, p := range allPrompts {
		name := strings.TrimSuffix(filepath.Base(p.Path), ".md")
		if name == promptName {
			content = p.Template
			break
		}
	}

	if content == "" {
		return nil, fmt.Errorf("prompt template %q not found", promptName)
	}

	return &slashResult{expandInput: content}, nil
}


// buildProviderFromNameAndModel creates an llm.Provider for /model handoffs.
func buildProviderFromNameAndModel(providerName, model string, cfg *config.Config) llm.Provider {
	switch providerName {
	case "openai":
		return llm.NewOpenAIProviderWithKey(cfg.OpenAIBaseURL, model, cfg.OpenAIAPIKey)
	case "anthropic":
		return llm.NewAnthropicProvider(cfg.AnthropicAPIKey, model)
	case "llamacpp", "llama.cpp":
		return llm.NewLlamaCppProvider(cfg.LlamaCppBaseURL)
	default: // "ollama" or anything else
		return llm.NewOllamaProvider(cfg.OllamaBaseURL, model)
	}
}

// BangCommandResult holds the result of a !command execution.
type BangCommandResult struct {
	Output  string
	IsError bool
}

// HandleBangCommand executes a shell command from a "!cmd" or "!!cmd" input.
// Single "!" → paste output into editor.
// Double "!!" → send output directly to the model.
func HandleBangCommand(raw string) (result BangCommandResult, sendDirectly bool) {
	if strings.HasPrefix(raw, "!!") {
		sendDirectly = true
		raw = raw[2:]
	} else {
		raw = raw[1:]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}

	argsJSON := []byte(`{"command":` + jsonQuote(raw) + `}`)
	t := tools.Bash{}
	res, err := t.Execute(context.Background(), argsJSON, nil)
	if err != nil {
		result.Output = "Error: " + err.Error()
		result.IsError = true
		return
	}
	result.Output = res.Content
	result.IsError = res.IsError
	return
}

// jsonQuote produces a JSON-safe double-quoted string from s.
func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}
