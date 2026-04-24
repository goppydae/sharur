package interactive

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/session"

	lipgloss "charm.land/lipgloss/v2"
)

// modalKind identifies the type of modal overlay.
type modalKind int

const (
	modalNone modalKind = iota
	modalStats
	modalConfig
	modalTree
)

const treePageSize = 8 // nodes per page in the tree modal

// modalState holds the state of a modal overlay.
type modalState struct {
	kind    modalKind
	content string // pre-rendered text content
	title   string
	visible bool
	cursor  int // for interactive modals (like tree)
	offset  int // scroll offset for paginated modals
	nodes   []session.FlatNode
}

// newModal creates an idle modal state.
func newModal() modalState {
	return modalState{
		kind:    modalNone,
		visible: false,
	}
}

func (m *modalState) openStatsModal(stats agent.AgentStats, style Style) {
	bg := style.PanelBgColor()
	header := lipgloss.NewStyle().Foreground(style.AccentColor()).Background(bg).Bold(true).Underline(true)

	var sb strings.Builder
	addKV := func(k, v string) {
		renderKV(&sb, k, v, 15, style)
	}

	sb.WriteString(header.Render("Session Info"))
	sb.WriteString("\n\n")
	if stats.Name != "" {
		addKV("Name:", stats.Name)
	}
	addKV("ID:", stats.SessionID)
	if stats.ParentID != "" {
		addKV("Parent ID:", stats.ParentID)
	}
	addKV("File:", filepath.Base(stats.SessionFile))
	addKV("Path:", filepath.Dir(stats.SessionFile))
	addKV("Created:", stats.CreatedAt.Format("Jan 02 15:04:05"))
	addKV("Updated:", stats.UpdatedAt.Format("Jan 02 15:04:05"))
	addKV("Model:", stats.Model)
	addKV("Provider:", stats.Provider)
	addKV("Thinking:", stats.Thinking)
	sb.WriteString("\n")

	sb.WriteString(header.Render("Messages"))
	sb.WriteString("\n\n")
	addKV("User:", strconv.Itoa(stats.UserMessages))
	addKV("Assistant:", strconv.Itoa(stats.AssistantMsgs))
	addKV("Tool Calls:", strconv.Itoa(stats.ToolCalls))
	addKV("Tool Results:", strconv.Itoa(stats.ToolResults))
	addKV("Total:", strconv.Itoa(stats.TotalMessages))
	sb.WriteString("\n")

	sb.WriteString(header.Render("Tokens"))
	sb.WriteString("\n\n")
	addKV("Input:", strconv.Itoa(stats.InputTokens))
	addKV("Output:", strconv.Itoa(stats.OutputTokens))
	if stats.CacheRead > 0 {
		addKV("Cache Read:", strconv.Itoa(stats.CacheRead))
	}
	if stats.CacheWrite > 0 {
		addKV("Cache Write:", strconv.Itoa(stats.CacheWrite))
	}
	addKV("Turn Total:", strconv.Itoa(stats.TotalTokens))
	addKV("Context:", fmt.Sprintf("%d / %d", stats.ContextTokens, stats.ContextWindow))

	if stats.Cost > 0 {
		sb.WriteString("\n")
		sb.WriteString(header.Render("Cost"))
		sb.WriteString("\n\n")
		addKV("Total:", fmt.Sprintf("$%.4f", stats.Cost))
	}

	m.kind = modalStats
	m.title = "Session Stats"
	m.content = sb.String()
	m.visible = true
}

func (m *modalState) openConfigModal(model, provider, thinking, theme, mode string,
	ollamaURL, openaiURL, anthropicKeySet, llamacppURL string,
	compactionEnabled bool, reserveTokens, keepRecentTokens int, style Style) {

	bg := style.PanelBgColor()
	header := lipgloss.NewStyle().Foreground(style.AccentColor()).Background(bg).Bold(true).Underline(true)

	var sb strings.Builder
	addKV := func(k, v string) {
		renderKV(&sb, k, v, 20, style)
	}

	sb.WriteString(header.Render("Core"))
	sb.WriteString("\n\n")
	addKV("Model:", model)
	addKV("Provider:", provider)
	addKV("Thinking:", thinking)
	addKV("Theme:", theme)
	addKV("Mode:", mode)
	sb.WriteString("\n")

	sb.WriteString(header.Render("API Base URLs"))
	sb.WriteString("\n\n")
	addKV("Ollama:", ollamaURL)
	addKV("OpenAI:", openaiURL)
	addKV("Anthropic:", anthropicKeySet)
	addKV("llama.cpp:", llamacppURL)
	sb.WriteString("\n")

	sb.WriteString(header.Render("Context Compaction"))
	sb.WriteString("\n\n")
	compState := "disabled"
	if compactionEnabled {
		compState = "enabled"
	}
	addKV("Status:", compState)
	addKV("Reserve Tokens:", strconv.Itoa(reserveTokens))
	addKV("Keep Recent:", strconv.Itoa(keepRecentTokens))

	m.kind = modalConfig
	m.title = "Configuration"
	m.content = sb.String()
	m.visible = true
}

func renderKV(sb *strings.Builder, k, v string, width int, style Style) {
	bg := style.PanelBgColor()
	bold := lipgloss.NewStyle().Bold(true).Background(bg)
	valStyle := lipgloss.NewStyle().Background(bg)

	sb.WriteString(bold.Render(fmt.Sprintf("%-*s", width, k)))
	sb.WriteString(valStyle.Render(" " + v))
	sb.WriteString("\n")
}

// close hides the modal.
func (m *modalState) close() {
	m.kind = modalNone
	m.visible = false
	m.content = ""
}

// render draws the modal as a centered box.
func (m *modalState) render(width, height int, style Style) string {
	if !m.visible {
		return ""
	}

	// Modal dimensions: 80% of screen, min 60, max 120
	modalW := width * 8 / 10
	if modalW < 60 {
		modalW = width
	}
	if modalW > 120 {
		modalW = 120
	}

	// Border style
	borderStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(style.AccentColor()).
		Background(style.PanelBgColor()).
		Padding(1, 2)

	// Inner width accounts for 2 cells border + 4 cells padding
	innerW := modalW - 6

	// Title style
	titleStyle := lipgloss.NewStyle().
		Foreground(style.PanelBgColor()).
		Background(style.AccentColor()).
		Bold(true).
		Padding(0, 1).
		Width(innerW)

	// Content style
	contentStyle := lipgloss.NewStyle().
		Foreground(style.MutedTextColor()).
		Background(style.PanelBgColor())

	// Render title
	titleBlock := titleStyle.Render(m.title)

	// Render content
	contentLines := strings.Split(m.content, "\n")
	var renderedLines []string
	for _, line := range contentLines {
		if m.kind == modalTree {
			// Truncate instead of wrapping for tree to preserve structure
			if lipgloss.Width(line) > innerW {
				line = line[:innerW] // Note: rough truncation, ideally use a rune-aware version
			}
			renderedLines = append(renderedLines, contentStyle.Width(innerW).Render(line))
		} else {
			wrapped := lipgloss.Wrap(line, innerW, "")
			for _, wl := range strings.Split(wrapped, "\n") {
				renderedLines = append(renderedLines, contentStyle.Width(innerW).Render(wl))
			}
		}
	}

	// Build the inner layout
	// Spacer also needs to fill the width with the background
	spacer := contentStyle.Width(innerW).Render("")

	inner := lipgloss.JoinVertical(lipgloss.Left,
		titleBlock,
		spacer,
		strings.Join(renderedLines, "\n"),
	)

	// Apply border and final dimensions
	res := borderStyle.Width(modalW).Render(inner)

	// Truncate if height exceeds screen
	if lipgloss.Height(res) > height-2 {
		res = borderStyle.Width(modalW).Height(height - 2).Render(inner)
	}

	return res
}

// openTreeModal builds the session tree overlay content.
func (m *modalState) openTreeModal(nodes []session.FlatNode, currentID string, style Style) {
	m.kind = modalTree
	m.title = "Session Tree"
	m.nodes = nodes
	m.visible = true
	m.cursor = 0

	// Set cursor to current session if found
	for i, n := range nodes {
		if n.Node.ID == currentID {
			m.cursor = i
			break
		}
	}

	// Initialize offset to keep cursor in view
	m.offset = 0
	if m.cursor >= treePageSize {
		m.offset = m.cursor - treePageSize + 1
	}

	m.refreshTreeContent(currentID, style)
}

func (m *modalState) refreshTreeContent(currentID string, style Style) {
	bg := style.PanelBgColor()
	var sb strings.Builder

	// Header Row
	col1Width := 55
	col2Width := 25
	metaStyle := lipgloss.NewStyle().Background(bg).Foreground(style.MutedTextColor()).Italic(true)
	headerStyle := lipgloss.NewStyle().Background(bg).Foreground(style.AccentColor()).Bold(true)

	h1 := headerStyle.Width(col1Width).Render(" SESSION")
	h2 := headerStyle.Width(col2Width).Render("DESCRIPTION")
	h3 := headerStyle.Width(14).Render("CREATED")
	h4 := headerStyle.Width(14).Render("UPDATED")
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, h1, "  ", h2, "  ", h3, "  ", h4))
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(style.Bordered()).Background(bg).Render(strings.Repeat("─", col1Width+col2Width+14+14+6)))
	sb.WriteString("\n")

	end := m.offset + treePageSize
	if end > len(m.nodes) {
		end = len(m.nodes)
	}

	for i := m.offset; i < end; i++ {
		n := m.nodes[i]

		// Selection cursor / active-session marker
		cursor := "  "
		cursorStyle := lipgloss.NewStyle().Background(bg).Foreground(style.MutedTextColor())
		if i == m.cursor {
			cursor = "› "
			cursorStyle = cursorStyle.Foreground(style.AccentColor())
		}

		// Build prefix with gutters at their correct positions
		displayIndent := n.Indent

		var prefix strings.Builder
		gutterMap := make(map[int]bool)
		for _, g := range n.Gutters {
			if g.Show {
				gutterMap[g.Position] = true
			}
		}

		connectorPos := -1
		if n.ShowConnector {
			connectorPos = displayIndent - 1
		}

		for l := 0; l < displayIndent; l++ {
			if l == connectorPos {
				if n.IsLast {
					prefix.WriteString("└─")
				} else {
					prefix.WriteString("├─")
				}
			} else if gutterMap[l] {
				prefix.WriteString("│ ")
			} else {
				prefix.WriteString("  ")
			}
		}

		prefixStr := prefix.String()
		activeMarker := " "
		activeStyle := lipgloss.NewStyle().Background(bg)
		if n.Node.ID == currentID {
			activeMarker = "•"
			activeStyle = activeStyle.Foreground(style.AccentColor())
		}

		// Label: ID or Name
		label := n.Node.ID
		if n.Node.Name != "" {
			label = n.Node.Name
		}
		labelStyle := lipgloss.NewStyle().Background(bg).Foreground(style.MutedTextColor())
		if i == m.cursor {
			labelStyle = labelStyle.Foreground(style.AccentColor()).Bold(true)
		}

		// First message snippet
		firstMsg := strings.ReplaceAll(strings.TrimSpace(n.Node.FirstMessage), "\n", " ")
		if firstMsg == "" {
			firstMsg = "(empty)"
		}

		// Column 1: Cursor + Tree + Active + Label (Fixed 55 chars)
		prefixStyle := lipgloss.NewStyle().Background(bg).Foreground(style.MutedTextColor())

		cStr := cursorStyle.Render(cursor)
		pStr := prefixStyle.Render(prefixStr)
		aStr := activeStyle.Render(activeMarker)
		lStr := labelStyle.Render(label)

		c1Content := aStr + cStr + pStr + " " + lStr
		if lipgloss.Width(c1Content) > col1Width {
			// Truncate label if necessary to fit
			targetLabelWidth := col1Width - lipgloss.Width(cStr+pStr+aStr) - 3
			if targetLabelWidth > 0 {
				labelRunes := []rune(label)
				if len(labelRunes) > targetLabelWidth {
					label = string(labelRunes[:targetLabelWidth]) + "..."
				}
				lStr = labelStyle.Render(label)
				c1Content = cStr + pStr + aStr + lStr
			} else {
				c1Content = c1Content[:col1Width-3] + "..."
			}
		}
		// Pad to fixed width
		c1 := lipgloss.NewStyle().Background(bg).Width(col1Width).Render(c1Content)

		// Column 2: Snippet (Fixed 25 chars)
		if lipgloss.Width(firstMsg) > col2Width {
			firstMsg = firstMsg[:col2Width-3] + "..."
		}
		c2 := metaStyle.Width(col2Width).Render(firstMsg)

		// Column 3: Created At (Fixed 14 chars)
		created := "-"
		if !n.Node.CreatedAt.IsZero() {
			created = n.Node.CreatedAt.Format("Jan 02 15:04")
		}
		c3 := metaStyle.Width(14).Render(created)

		// Column 4: Updated At (Fixed 14 chars)
		updated := "-"
		if !n.Node.UpdatedAt.IsZero() {
			updated = n.Node.UpdatedAt.Format("Jan 02 15:04")
		}
		c4 := metaStyle.Width(14).Render(updated)

		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, c1, "  ", c2, "  ", c3, "  ", c4))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	pagination := fmt.Sprintf("Page %d/%d (%d sessions total)", (m.offset/treePageSize)+1, (len(m.nodes)+treePageSize-1)/treePageSize, len(m.nodes))
	sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(style.MutedTextColor()).Italic(true).Render(pagination))
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(style.MutedTextColor()).Italic(true).Render("↑/↓: Navigate • Enter: Resume • B: Branch • Esc: Close"))

	m.content = sb.String()
}
