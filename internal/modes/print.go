// Package modes provides the four mode implementations.
package modes

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/config"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/modes/interactive"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/tools"
)

// Handler is the interface for mode implementations.
type Handler interface {
	Run(args []string) error
}

// PrintOptions holds optional startup options for PrintHandler.
type PrintOptions struct {
	NoSession      bool
	PreloadSession string // "continue", "resume", "fork:<path>"
	SystemPrompt   string
	ThinkingLevel  string
	JSON           bool
}


// NewPrintHandler creates a print mode handler.
func NewPrintHandler(provider llm.Provider, registry *tools.ToolRegistry, mgr *session.Manager, exts []agent.Extension, opts PrintOptions) Handler {
	return &PrintHandler{
		Provider:   provider,
		Registry:   registry,
		Manager:    mgr,
		Extensions: exts,
		Options:    opts,
	}
}

// NewInteractiveHandler creates an interactive mode handler.
func NewInteractiveHandler(provider llm.Provider, registry *tools.ToolRegistry, mgr *session.Manager, cfg *config.Config, theme string, exts []agent.Extension, opts interactive.Options) Handler {
	return &InteractiveHandler{
		Provider:   provider,
		Registry:   registry,
		Manager:    mgr,
		Config:     cfg,
		Theme:      theme,
		Extensions: exts,
		Options:    opts,
	}
}

// PrintHandler handles print mode (single-shot output).
type PrintHandler struct {
	Provider   llm.Provider
	Registry   *tools.ToolRegistry
	Manager    *session.Manager
	Extensions []agent.Extension
	Options    PrintOptions
}

// InteractiveHandler handles interactive mode (bubbletea TUI).
type InteractiveHandler struct {
	Provider   llm.Provider
	Registry   *tools.ToolRegistry
	Manager    *session.Manager
	Config     *config.Config
	Theme      string
	Extensions []agent.Extension
	Options    interactive.Options
}

func (h *InteractiveHandler) Run(args []string) error {
	theme := h.Theme
	if theme == "" {
		theme = "dark"
	}
	return interactive.Run(h.Provider, h.Registry, h.Manager, h.Config, theme, h.Extensions, h.Options, args)
}

func (h *PrintHandler) Run(args []string) error {
	prompt, fileData, err := buildPrompt(args)
	if err != nil {
		return err
	}
	if prompt == "" && fileData == "" {
		return fmt.Errorf("no prompt provided — pass text or pipe stdin")
	}

	// Merge @file contents + inline prompt
	full := mergePrompt(prompt, fileData)

	ag := agent.New(h.Provider, h.Registry)
	ag.SetExtensions(h.Extensions)
	if h.Options.ThinkingLevel != "" {
		ag.SetThinkingLevel(agent.ThinkingLevel(h.Options.ThinkingLevel))
	}

	// Session management
	var sess *session.Session
	if !h.Options.NoSession {
		var serr error
		switch {
		case strings.HasPrefix(h.Options.PreloadSession, "fork:"):
			id := strings.TrimPrefix(h.Options.PreloadSession, "fork:")
			source, lerr := h.Manager.Load(id)
			if lerr == nil {
				sess, serr = h.Manager.Fork(source)
			} else {
				serr = lerr
			}
		case h.Options.PreloadSession == "continue":
			list, lerr := h.Manager.List()
			if lerr == nil && len(list) > 0 {
				sess, serr = h.Manager.Load(list[len(list)-1])
			} else {
				sess, serr = h.Manager.Create()
			}
		case strings.HasPrefix(h.Options.PreloadSession, "resume:"):
			id := strings.TrimPrefix(h.Options.PreloadSession, "resume:")
			sess, serr = h.Manager.Load(id)
		case h.Options.PreloadSession != "" && h.Options.PreloadSession != "resume":
			// Literal path or ID
			sess, serr = h.Manager.Load(h.Options.PreloadSession)
			if serr != nil {
				sess, serr = h.Manager.LoadPath(h.Options.PreloadSession)
			}
		default:
			sess, serr = h.Manager.Create()
		}

		if serr == nil && sess != nil {
			ag.LoadSession(sess.ToTypes())
		}
	}

	if h.Options.SystemPrompt != "" {
		ag.SetSystemPrompt(h.Options.SystemPrompt)
	}

	ag.Subscribe(func(e agent.Event) {
		if h.Options.JSON {
			switch e.Type {
			case agent.EventMessageStart:
				writeJSON(os.Stdout, map[string]any{"type": "message_start"})
			case agent.EventTextDelta:
				writeJSON(os.Stdout, map[string]any{
					"type":    "text_delta",
					"content": e.Content,
				})
			case agent.EventToolCall:
				writeJSON(os.Stdout, map[string]any{
					"type":     "tool_call",
					"toolCall": formatToolCall(e.ToolCall),
				})
			case agent.EventMessageEnd:
				ev := map[string]any{"type": "message_end"}
				if e.Usage != nil {
					ev["usage"] = e.Usage
				}
				writeJSON(os.Stdout, ev)
			case agent.EventAgentStart:
				writeJSON(os.Stdout, map[string]any{"type": "agent_start"})
			case agent.EventAgentEnd:
				writeJSON(os.Stdout, map[string]any{"type": "agent_end"})
			case agent.EventTurnStart:
				writeJSON(os.Stdout, map[string]any{"type": "turn_start"})
			case agent.EventAbort:
				writeJSON(os.Stdout, map[string]any{"type": "abort"})
			case agent.EventError:
				msg := ""
				if e.Error != nil {
					msg = e.Error.Error()
				}
				writeJSON(os.Stderr, map[string]any{"type": "error", "error": msg})
			}
			return
		}

		switch e.Type {
		case agent.EventTextDelta:
			fmt.Print(e.Content)
		case agent.EventError:
			fmt.Fprintf(os.Stderr, "\nError: %v\n", e.Error)
		}
	})

	ctx := context.Background()
	if err := ag.Prompt(ctx, full); err != nil {
		return err
	}
	<-ag.Idle()
	fmt.Println()

	// Save session if not ephemeral
	if !h.Options.NoSession && sess != nil {
		updated := ag.GetSession()
		sess.Messages = updated.Messages
		sess.UpdatedAt = time.Now()
		if err := h.Manager.Save(sess); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not save session: %v\n", err)
		}
	}

	return nil
}

// buildPrompt resolves args into a prompt string and any @file contents.
// @file args are expanded to their file contents; plain args become the prompt text.
func buildPrompt(args []string) (prompt, fileData string, err error) {
	var promptParts []string
	var fileParts []string

	// Check stdin
	if !isTerminal(os.Stdin) {
		scanner := bufio.NewScanner(os.Stdin)
		var sb strings.Builder
		for scanner.Scan() {
			sb.WriteString(scanner.Text())
			sb.WriteByte('\n')
		}
		if scanner.Err() != nil {
			return "", "", fmt.Errorf("read stdin: %w", scanner.Err())
		}
		if text := strings.TrimSpace(sb.String()); text != "" {
			fileParts = append(fileParts, text)
		}
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "@") {
			path := arg[1:]
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", "", fmt.Errorf("read %s: %w", path, readErr)
			}
			fileParts = append(fileParts, fmt.Sprintf("--- %s ---\n%s", path, string(data)))
		} else {
			promptParts = append(promptParts, arg)
		}
	}

	return strings.Join(promptParts, " "), strings.Join(fileParts, "\n\n"), nil
}

func mergePrompt(prompt, fileData string) string {
	switch {
	case fileData == "":
		return prompt
	case prompt == "":
		return fileData
	default:
		return fileData + "\n\n" + prompt
	}
}

// isTerminal reports whether f is connected to a terminal (not a pipe/file).
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}


