package interactive

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/goppydae/gollm/internal/config"
	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/themes"
)

// Options holds optional startup options for interactive mode.
type Options struct {
	NoSession      bool
	PreloadSession string // "continue", "resume", "fork:<path>"
}

// Run starts the interactive Bubble Tea UI.
func Run(client pb.AgentServiceClient, sessionID string, cfg *config.Config, themeName string, opts Options, args []string) error {
	// Manager retained for file I/O only (/import, /export, session path display).
	mgr := session.NewManager(cfg.SessionDir)

	theme := loadTheme(themeName, cfg.ThemePaths)

	var modelName, providerName string
	var contextWindow int
	if info, err := client.GetState(context.Background(), &pb.GetStateRequest{SessionId: sessionID}); err == nil {
		modelName = info.Model
		providerName = info.Provider
		if info.ProviderInfo != nil {
			contextWindow = int(info.ProviderInfo.ContextWindow)
		}
	} else {
		modelName = cfg.Model
		providerName = cfg.Provider
	}

	eventCh := make(chan *pb.AgentEvent, 1024)

	initialInput := ""
	if opts.PreloadSession == "resume" {
		initialInput = "/resume "
	} else if len(args) > 0 {
		initialInput = strings.Join(args, " ")
	}

	style := themes.NewStyle(*theme)
	m := newModel(modelName, providerName, string(cfg.ThinkingLevel), contextWindow, client, sessionID, eventCh, mgr, cfg, initialInput, style)
	m.syncHistoryFromService()
	if opts.PreloadSession == "continue" && len(m.history) > 0 {
		m.history = append(m.history, historyEntry{
			role:  "info",
			items: []contentItem{{kind: contentItemText, text: "Resumed session: " + sessionID}},
		})
	}
	m.models = cfg.Models
	m.modelIndex = 0

	p := tea.NewProgram(m)
	_, err := p.Run()
	m.cancel()
	return err
}

func loadTheme(name string, paths []string) *themes.Theme {
	for _, p := range paths {
		for _, ext := range []string{"", ".json", ".yaml", ".yml"} {
			t, err := themes.LoadTheme(filepath.Join(p, name+ext))
			if err == nil {
				return t
			}
		}
	}
	bundled := themes.Bundled()
	if t, ok := bundled[name]; ok {
		return t
	}
	return bundled["dark"]
}
