package interactive

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goppydae/gollm/internal/session"
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	lipgloss "charm.land/lipgloss/v2"
	"charm.land/huh/v2"
)

// modalKind identifies the type of modal overlay.
type modalKind int

const (
	modalNone modalKind = iota
	modalStats
	modalConfig
	modalTree
	modalModels
	modalHelp
	modalRebase
)


// modalState holds the state of a modal overlay.
type modalState struct {
	kind    modalKind
	title   string
	visible bool
	table   table.Model
	list    list.Model
	form    *huh.Form
}

// treeItem implements list.Item for the session tree.
type treeItem struct {
	node session.FlatNode
}

func (i treeItem) Title() string       { return i.node.Node.Role + ": " + i.node.Node.Content }
func (i treeItem) Description() string { return i.node.Node.ID }
func (i treeItem) FilterValue() string { return i.node.Node.Role + " " + i.node.Node.Content }

// modelItem implements list.Item for the model selection.
type modelItem struct {
	name     string
	provider string
}

func (i modelItem) Title() string       { return i.name }
func (i modelItem) Description() string { return i.provider }
func (i modelItem) FilterValue() string { return i.name + " " + i.provider }

// rebaseItem implements list.Item for the interactive rebase picker.
// Each item represents one message in the session with its keep/squash toggle state.
type rebaseItem struct {
	index   int
	role    string
	content string // truncated preview
	checked bool   // keep this message
	squash  bool   // condense via LLM (implies keep)
}

func (i rebaseItem) Title() string       { return i.content }
func (i rebaseItem) Description() string { return "" }
func (i rebaseItem) FilterValue() string { return i.content }

type rebaseDelegate struct {
	style Style
}

func (d rebaseDelegate) Height() int                               { return 1 }
func (d rebaseDelegate) Spacing() int                              { return 0 }
func (d rebaseDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd  { return nil }

func (d rebaseDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	ri, ok := listItem.(rebaseItem)
	if !ok {
		return
	}

	bg := d.style.PanelBgColor()
	base := lipgloss.NewStyle().Background(bg)

	cursor := "  "
	if index == m.Index() {
		cursor = "› "
	}

	var check string
	switch {
	case ri.squash:
		check = "[S]"
	case ri.checked:
		check = "[x]"
	default:
		check = "[ ]"
	}

	roleStr := fmt.Sprintf("%-9s", ri.role)
	line := fmt.Sprintf("%s %s #%-3d %s: %s", cursor, check, ri.index+1, roleStr, ri.content)

	var style lipgloss.Style
	if index == m.Index() {
		style = base.Foreground(d.style.AccentColor())
	} else if !ri.checked && !ri.squash {
		style = base.Foreground(d.style.MutedTextColor())
	} else {
		style = base.Foreground(d.style.AccentTextColor())
	}
	_, _ = fmt.Fprint(w, style.Render(line))
}

// openRebaseModal opens an interactive message-picker modal for rebasing.
// All messages are checked=true by default; the user opts out.
func (m *modalState) openRebaseModal(messages []rebaseItem, style Style) {
	m.kind = modalRebase
	m.title = "Rebase: [space] keep/drop  [s] squash  [a] toggle all  [enter] confirm"
	m.visible = true

	items := make([]list.Item, len(messages))
	for i, ri := range messages {
		items[i] = ri
	}

	l := list.New(items, rebaseDelegate{style: style}, 0, 0)
	l.SetShowHelp(false)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.KeyMap.Quit.Unbind()
	m.list = l
}

// rebaseCheckedIndices returns two slices: indices to keep and indices to squash.
func (m *modalState) rebaseCheckedIndices() (keep []int32, squash []int32) {
	for _, item := range m.list.Items() {
		ri, ok := item.(rebaseItem)
		if !ok {
			continue
		}
		if ri.squash {
			squash = append(squash, int32(ri.index))
		} else if ri.checked {
			keep = append(keep, int32(ri.index))
		}
	}
	return
}

// newModal creates an idle modal state.
func newModal() modalState {
	return modalState{
		kind:    modalNone,
		visible: false,
	}
}

func (m *modalState) openStatsModal(stats agentStats, style Style) {
	m.kind = modalStats
	m.title = "Session Stats"
	m.visible = true

	columns := []table.Column{
		{Title: "Property", Width: 20},
		{Title: "Value", Width: 40},
	}

	rows := []table.Row{
		{"ID", stats.SessionID},
		{"File", filepath.Base(stats.SessionFile)},
		{"Created", stats.CreatedAt.Format("Jan 02 15:04:05")},
		{"Updated", stats.UpdatedAt.Format("Jan 02 15:04:05")},
		{"Model", stats.Model},
		{"Provider", stats.Provider},
		{"Thinking", stats.Thinking},
		{"---", "---"},
		{"User Msg", strconv.Itoa(stats.UserMessages)},
		{"Assistant Msg", strconv.Itoa(stats.AssistantMsgs)},
		{"Total Msg", strconv.Itoa(stats.TotalMessages)},
		{"---", "---"},
		{"Input Tokens", strconv.Itoa(stats.InputTokens)},
		{"Output Tokens", strconv.Itoa(stats.OutputTokens)},
		{"Total Tokens", strconv.Itoa(stats.TotalTokens)},
		{"Context", fmt.Sprintf("%d / %d", stats.ContextTokens, stats.ContextWindow)},
	}

	if stats.Cost > 0 {
		rows = append(rows, table.Row{"Cost", fmt.Sprintf("$%.4f", stats.Cost)})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(len(rows)+1), // header + all data rows; clamped to viewport in render
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(style.AccentColor()).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(style.AccentColor()).
		Bold(false)
	t.SetStyles(s)

	m.table = t
}

func (m *modalState) openConfigModal(model, provider, thinking, theme, mode string,
	ollamaURL, openaiURL, anthropicKeySet, llamacppURL string,
	compactionEnabled bool, reserveTokens, keepRecentTokens int, style Style) {

	m.kind = modalConfig
	m.title = "Configuration"
	m.visible = true

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("model").
				Title("Model").
				Value(&model),
			huh.NewSelect[string]().
				Key("provider").
				Title("Provider").
				Options(
					huh.NewOption("ollama", "ollama"),
					huh.NewOption("openai", "openai"),
					huh.NewOption("anthropic", "anthropic"),
					huh.NewOption("google", "google"),
					huh.NewOption("llamacpp", "llamacpp"),
				).
				Value(&provider),
			huh.NewSelect[string]().
				Key("thinking").
				Title("Thinking Level").
				Options(
					huh.NewOption("none", "none"),
					huh.NewOption("low", "low"),
					huh.NewOption("medium", "medium"),
					huh.NewOption("high", "high"),
				).
				Value(&thinking),
			huh.NewInput().
				Key("theme").
				Title("Theme").
				Value(&theme),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Key("compaction").
				Title("Compaction Enabled").
				Value(&compactionEnabled),
			huh.NewInput().
				Key("reserve").
				Title("Reserve Tokens").
				Validate(func(s string) error {
					_, err := strconv.Atoi(s)
					return err
				}).
				Value(ptr(strconv.Itoa(reserveTokens))),
		),
	).WithWidth(40) // Default width
}

// treeDelegate handles rendering for session tree items.
type treeDelegate struct {
	style     Style
	currentID string
}

func (d treeDelegate) Height() int                               { return 1 } //nolint:unused
func (d treeDelegate) Spacing() int                              { return 0 } //nolint:unused
func (d treeDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil } //nolint:unused
func (d treeDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) { //nolint:unused
	i, ok := listItem.(treeItem)
	if !ok {
		return
	}

	n := i.node
	bg := d.style.PanelBgColor()
	selected := index == m.Index()

	// Build tree prefix string (box-drawing chars only).
	var prefix strings.Builder
	gutterMap := make(map[int]bool)
	for _, g := range n.Gutters {
		if g.Show {
			gutterMap[g.Position] = true
		}
	}
	connectorPos := -1
	if n.ShowConnector {
		connectorPos = n.Indent - 1
	}
	for l := 0; l < n.Indent; l++ {
		switch {
		case l == connectorPos:
			if n.IsLast {
				prefix.WriteString("└─ ")
			} else {
				prefix.WriteString("├─ ")
			}
		case gutterMap[l]:
			prefix.WriteString("│  ")
		default:
			prefix.WriteString("   ")
		}
	}

	// Message content preview
	content := n.Node.Content
	content = strings.ReplaceAll(content, "\n", " ")
	if len(content) > 80 {
		content = content[:77] + "..."
	}

	// Role label with color coding
	roleLabel := n.Node.Role
	if roleLabel == "" {
		roleLabel = "record"
	}
	roleStr := fmt.Sprintf("[%s]", roleLabel)
	var roleStyle lipgloss.Style
	switch n.Node.Role {
	case "user":
		roleStyle = lipgloss.NewStyle().Foreground(d.style.SuccessColor())
	case "assistant":
		roleStyle = lipgloss.NewStyle().Foreground(d.style.AccentColor())
	case "compaction", "summary":
		roleStyle = lipgloss.NewStyle().Foreground(d.style.WorkingColor())
	default:
		roleStyle = lipgloss.NewStyle().Foreground(d.style.MutedTextColor())
	}

	// Active-path marker
	marker := "  "
	if n.Node.IsActive {
		marker = "● "
	}

	base := lipgloss.NewStyle().Background(bg)
	var lineStyle lipgloss.Style
	var cursor string
	if selected {
		lineStyle = base.Foreground(d.style.AccentTextColor()).Bold(true)
		cursor = base.Render("› ")
	} else {
		lineStyle = base.Foreground(d.style.AccentTextColor())
		cursor = base.Render("  ")
	}

	prefixStr := base.Foreground(d.style.MutedTextColor()).Render(prefix.String())
	markerStr := base.Render(marker)
	rolePart := roleStyle.Background(bg).Width(11).Render(roleStr)
	contentPart := lineStyle.Render(content)

	line := fmt.Sprintf("%s%s %s", markerStr, rolePart, contentPart)
	_, _ = fmt.Fprint(w, cursor+prefixStr+line)
}

// close hides the modal.
func (m *modalState) close() {
	m.kind = modalNone
	m.visible = false
}

// render draws the modal as a centered box.
func (m *modalState) render(width, height int, style Style, h help.Model, k KeyMap) string {
	if !m.visible {
		return ""
	}

	const pad = 4
	maxW := width - pad*2
	maxH := height - pad*2

	modalW := width * 9 / 10
	if modalW > maxW { modalW = maxW }
	if modalW > 140  { modalW = 140 }
	modalH := height * 8 / 10
	if modalH > maxH { modalH = maxH }

	var content string
	switch m.kind {
	case modalStats:
		// Shrink/expand modalH to exactly fit all rows when the viewport allows.
		// Chrome overhead: 2 border + 2 padding (Padding(1,2)) + 1 title + 1 blank = 6.
		idealH := len(m.table.Rows()) + 1 + 6 // data rows + header + chrome
		if idealH < maxH {
			modalH = idealH
		} else {
			modalH = maxH
		}
		m.table.SetWidth(modalW - 6)
		m.table.SetHeight(modalH - 6)
		content = m.table.View()
	case modalConfig:
		m.form.WithWidth(modalW - 6)
		content = m.form.View()
	case modalTree, modalModels, modalRebase:
		m.list.SetSize(modalW-6, modalH-6)
		content = m.list.View()
	case modalHelp:
		// help uses the helper bubble
		h.ShowAll = true // Always show full help in modal
		content = h.View(k)
	}

	borderStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(style.AccentColor()).
		Background(style.PanelBgColor()).
		Padding(1, 2)

	innerW := modalW - 6
	titleStyle := lipgloss.NewStyle().
		Foreground(style.PanelBgColor()).
		Background(style.AccentColor()).
		Bold(true).
		Padding(0, 1).
		Width(innerW)

	titleBlock := titleStyle.Render(m.title)

	var inner string
	if m.kind == modalTree {
		footerStyle := lipgloss.NewStyle().
			Foreground(style.MutedTextColor()).
			Background(style.PanelBgColor()).
			Width(innerW)
		footer := footerStyle.Render("enter: resume   b: branch   f: fork   r: rebase   esc: close")
		inner = lipgloss.JoinVertical(lipgloss.Left, titleBlock, "", content, "", footer)
	} else {
		inner = lipgloss.JoinVertical(lipgloss.Left, titleBlock, "", content)
	}

	return borderStyle.Width(modalW).Render(inner)
}

// openTreeModal builds the session tree overlay content.
func (m *modalState) openTreeModal(nodes []session.FlatNode, currentID string, style Style) {
	m.kind = modalTree
	m.title = "Session Tree"
	m.visible = true

	items := make([]list.Item, len(nodes))
	startIndex := 0
	for i, n := range nodes {
		items[i] = treeItem{node: n}
		if n.Node.ID == currentID {
			startIndex = i
		}
	}

	l := list.New(items, treeDelegate{style: style, currentID: currentID}, 0, 0)
	l.SetShowHelp(false)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	if len(items) > 0 {
		l.Select(startIndex)
	}

	m.list = l
}

// modelDelegate handles rendering for model selection items.
type modelDelegate struct {
	style Style
}

func (d modelDelegate) Height() int                               { return 1 }
func (d modelDelegate) Spacing() int                              { return 0 }
func (d modelDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d modelDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(modelItem)
	if !ok {
		return
	}

	bg := d.style.PanelBgColor()
	str := fmt.Sprintf("  %s (%s)", i.name, i.provider)
	style := d.style.Muted().Background(bg)
	if index == m.Index() {
		style = d.style.StatusWorking().Background(bg).Bold(true)
		str = "> " + str[2:]
	}

	_, _ = fmt.Fprint(w, style.Render(str))
}

func (m *modalState) openModelsModal(availableModels []string, currentModel string, style Style) {
	m.kind = modalModels
	m.title = "Select Model"
	m.visible = true

	items := make([]list.Item, len(availableModels))
	startIndex := 0
	for i, mstr := range availableModels {
		name := mstr
		provider := "default"
		if idx := strings.IndexByte(mstr, '/'); idx >= 0 {
			provider = mstr[:idx]
			name = mstr[idx+1:]
		}
		items[i] = modelItem{name: name, provider: provider}
		if name == currentModel {
			startIndex = i
		}
	}

	l := list.New(items, modelDelegate{style: style}, 0, 0)
	l.SetShowHelp(false)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	if len(items) > 0 {
		l.Select(startIndex)
	}

	m.list = l
}

