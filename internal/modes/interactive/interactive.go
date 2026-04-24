package interactive

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/config"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/themes"
	"github.com/goppydae/gollm/internal/tools"
)

// Options holds optional startup options for interactive mode.
type Options struct {
	NoSession      bool
	PreloadSession string // "continue", "resume", "fork:<path>"
}

// Run starts the interactive Bubble Tea UI.
func Run(provider llm.Provider, registry *tools.ToolRegistry, mgr *session.Manager, cfg *config.Config, themeName string, exts []agent.Extension, opts Options, args []string) error {
	ag := agent.New(provider, registry)
	ag.SetThinkingLevel(agent.ThinkingLevel(cfg.ThinkingLevel))
	ag.SetExtensions(exts)

	// Pre-load session if requested
	if cfg.SessionPath != "" {
		opts.PreloadSession = cfg.SessionPath
	}

	if !opts.NoSession {
		var sess *session.Session
		var err error
		switch {
		case strings.HasPrefix(opts.PreloadSession, "fork:"):
			id := strings.TrimPrefix(opts.PreloadSession, "fork:")
			source, lerr := mgr.Load(id)
			if lerr == nil {
				sess, err = mgr.Fork(source)
			} else {
				err = lerr
			}
		case opts.PreloadSession == "continue":
			list, lerr := mgr.List()
			if lerr == nil && len(list) > 0 {
				id := list[len(list)-1]
				sess, err = mgr.Load(id)
				if err != nil {
					// Fallback to absolute path if ID lookup failed (shouldn't happen with the new list logic)
					if abs, err2 := filepath.Abs(id); err2 == nil {
						sess, err = mgr.Load(abs)
					}
				}
			}
			if sess == nil {
				sess, err = mgr.Create()
			}
		case strings.HasPrefix(opts.PreloadSession, "resume:"):
			id := strings.TrimPrefix(opts.PreloadSession, "resume:")
			sess, err = mgr.Load(id)
		case opts.PreloadSession == "resume":
			// We'll open the picker in the TUI, but we need a session to start with
			sess, err = mgr.Create()
		case opts.PreloadSession != "":
			sess, err = mgr.Load(opts.PreloadSession)
			if err != nil {
				sess, err = mgr.LoadPath(opts.PreloadSession)
			}
		default:
			sess, err = mgr.Create()
		}

		if err == nil && sess != nil {
			ag.LoadSession(sess.ToTypes())
		}
	}

	if cfg.SystemPrompt != "" {
		ag.SetSystemPrompt(cfg.SystemPrompt)
	}

	info := ag.GetInfo()
	theme := loadTheme(themeName, cfg.ThemePaths)

	eventCh := make(chan agent.Event, 1024)
	ag.Subscribe(func(ev agent.Event) {
		eventCh <- ev
	})

	initialInput := ""
	if opts.PreloadSession == "resume" {
		initialInput = "/resume "
	} else if len(args) > 0 {
		initialInput = strings.Join(args, " ")
	}

	m := newModel(info.Model, info.Name, string(cfg.ThinkingLevel), info.ContextWindow, ag, eventCh, mgr, cfg, initialInput)
	m.style = themes.NewStyle(*theme)
	m.syncHistoryFromAgent()
	m.models = cfg.Models
	m.modelIndex = 0

	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

func loadTheme(name string, paths []string) *themes.Theme {
	// Try loading from paths
	for _, p := range paths {
		// Try exact name, then with .json, then with .yaml
		for _, ext := range []string{"", ".json", ".yaml", ".yml"} {
			t, err := themes.LoadTheme(filepath.Join(p, name+ext))
			if err == nil {
				return t
			}
		}
	}
	// Fallback to bundled
	bundled := themes.Bundled()
	if t, ok := bundled[name]; ok {
		return t
	}
	return bundled["dark"]
}
