package interactive

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"

	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"github.com/goppydae/gollm/internal/config"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/themes"
)

// NewStyle creates a style from the given theme.
func NewStyle(t Theme) Style {
	return themes.NewStyle(t)
}

func newModel(modelName, provider, thinking string, contextWindow int, client pb.AgentServiceClient, sessionID string, eventCh chan *pb.AgentEvent, mgr *session.Manager, cfg *config.Config, initialInput string, style themes.Style) *model {
	// Input
	input := textarea.New()
	input.Placeholder = "Prompt me..."
	input.Prompt = ""
	if initialInput != "" && strings.HasPrefix(initialInput, "/") {
		input.SetValue(initialInput)
	}
	input.ShowLineNumbers = false
	input.SetHeight(inputHeight)
	input.Focus()

	styles := textarea.DefaultStyles(true)
	styles.Focused.Base = lipgloss.NewStyle()
	styles.Blurred.Base = lipgloss.NewStyle()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(
		compat.AdaptiveColor{Light: lipgloss.Color("#94a3b8"), Dark: lipgloss.Color("#64748b")})
	styles.Focused.CursorLine = lipgloss.NewStyle()
	input.SetStyles(styles)

	// Viewport
	vp := viewport.New(viewport.WithHeight(10))
	vp.Style = lipgloss.NewStyle()
	vp.SoftWrap = true

	// Spinner
	sp := spinner.New(spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("60"))))

	// Keymap
	keys := DefaultKeyMap()

	// Help
	h := help.New()
	h.Styles = help.DefaultDarkStyles()

	// Progress bar
	pg := progress.New(progress.WithWidth(30), progress.WithoutPercentage(), progress.WithScaled(true))
	pg.Full = progress.DefaultFullCharHalfBlock
	pg.FullColor = lipgloss.Color("60")
	pg.Empty = '░'
	pg.EmptyColor = lipgloss.Color("238")

	sw := stopwatch.New(stopwatch.WithInterval(time.Second))

	m := model{
		input:         input,
		vp:            vp,
		spinner:       sp,
		keys:          keys,
		helper:        h,
		progressBar:   pg,
		stopwatch:     sw,
		modelName:     modelName,
		provider:      provider,
		thinking:      thinking,
		contextWindow: contextWindow,
		history:       []historyEntry{},
		client:        client,
		sessionID:     sessionID,
		eventCh:       eventCh,
		sessionMgr:    mgr,
		config:        cfg,
		modal:         newModal(),
		picker:        newPickerList(style),
		initialPrompt: initialInput,
		historyIndex:  -1,
		style:         style,
	}

	m.newContext()
	if initialInput != "" {
		m.updatePicker()
	}
	return &m
}

func (m *model) newContext() {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
}

// Init implements tea.Model.
func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink, listenForEvent(m.eventCh), func() tea.Msg { return m.spinner.Tick() }}
	if m.initialPrompt != "" && !strings.HasPrefix(m.initialPrompt, "/") {
		cmds = append(cmds, func() tea.Msg { return initialPromptMsg{} })
	}
	return tea.Batch(cmds...)
}
