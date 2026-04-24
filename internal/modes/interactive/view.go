package interactive

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

var skillRegex = regexp.MustCompile(`(?s)<skill name="([^"]+)" location="([^"]+)">\n(.*?)\n<\/skill>(.*)`)
var fileRegex = regexp.MustCompile(`(?s)<file path="([^"]+)">\n(.*?)\n<\/file>(.*)`)

// View implements tea.Model.View.
func (m *model) View() tea.View {
	v := tea.NewView(m.layout())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *model) layout() string {
	// Chat area
	m.vp.SetContent(m.chatContent)
	chat := m.vp.View()

	// Separator
	separator := func() string {
		return lipgloss.NewStyle().
			Foreground(m.style.Bordered()).
			Render(strings.Repeat("─", m.width))
	}

	// Input
	input := m.input.View()

	// Footer
	footerText := lipgloss.NewStyle().
		Width(m.width).
		PaddingLeft(m.style.FooterPaddingX()).
		Render(renderFooter(m))

	sections := []string{chat, separator()}
	if m.picker.Open {
		sections = append(sections, m.picker.View(m.style))
	}
	sections = append(sections, input, separator(), footerText)
	mainStr := lipgloss.JoinVertical(lipgloss.Top, sections...)

	// Render modal overlay on top using Compositor
	if m.modal.visible {
		modalStr := m.modal.render(m.width, m.height, m.style)
		if modalStr != "" {
			bgLayer := lipgloss.NewLayer(mainStr)

			// Center the modal
			modalW := lipgloss.Width(modalStr)
			modalH := lipgloss.Height(modalStr)
			x := (m.width - modalW) / 2
			y := (m.height - modalH) / 2

			fgLayer := lipgloss.NewLayer(modalStr).X(x).Y(y).Z(1)

			return lipgloss.NewCompositor(bgLayer, fgLayer).Render()
		}
	}

	return mainStr
}


func (m *model) refreshViewport() *model {
	m.chatContent = m.buildChatContent()
	m.vp.SetContent(m.chatContent)
	if !m.userScrolled {
		m.vp.GotoBottom()
	}
	return m
}

func (m *model) buildChatContent() string {
	chatW := m.vp.Width()

	var lines []string
	for _, e := range m.history {
		lines = append(lines, renderEntry(e, m.style, chatW, m.toolCallsExpanded))
	}

	if m.isRunning {
		elapsed := m.stopwatch.Elapsed().String()
		lines = append(lines, m.style.WorkingIndicator().PaddingLeft(1).Render(m.spinner.View()+" Working... "+elapsed))
	}

	return strings.Join(lines, "\n")
}

func renderBox(lines []string, chatW, msgW int, boxStyle lipgloss.Style, alignRight bool) string {
	var out []string
	for _, line := range lines {
		out = append(out, boxStyle.MarginBottom(0).Width(msgW).Render(line))
	}
	
	// Top and bottom padding
	blank := boxStyle.MarginBottom(0).Width(msgW).Render("")
	
	if alignRight {
		leftPad := chatW - msgW
		padStr := strings.Repeat(" ", leftPad)
		
		res := padStr + blank + "\n"
		for _, line := range out {
			res += padStr + line + "\n"
		}
		res += padStr + blank
		return res
	}

	return blank + "\n" + strings.Join(out, "\n") + "\n" + blank
}

func wrapAndRightAlign(content string, chatW, msgW int, boxStyle lipgloss.Style) string {
	wrapped := lipgloss.Wrap(content, msgW-4, "")
	return renderBox(strings.Split(wrapped, "\n"), chatW, msgW, boxStyle, true)
}

func wrapAndLeftAlign(content string, chatW, msgW int, boxStyle lipgloss.Style) string {
	wrapped := lipgloss.Wrap(content, msgW-4, "")
	return renderBox(strings.Split(wrapped, "\n"), chatW, msgW, boxStyle, false)
}

func renderToolCall(tc toolCallEntry, output string, chatW int, s Style, expanded bool) string {
	var bgColor color.Color
	switch tc.status {
	case toolCallSuccess:
		bgColor = s.ToolSuccessBgColor()
	case toolCallFailure:
		bgColor = s.ToolFailureBgColor()
	default:
		bgColor = s.ToolRunningBgColor()
	}

	boxStyle := lipgloss.NewStyle().Background(bgColor).PaddingLeft(2).PaddingRight(2)
	
	var rawLines []string
	icon := "◌"
	if tc.status == toolCallSuccess { icon = "✓" }
	if tc.status == toolCallFailure { icon = "✗" }
	
	header := icon + " " + tc.name
	if tc.arg != "" { header += " " + tc.arg }
	rawLines = append(rawLines, s.ToolCall().Bold(true).Background(bgColor).Render(header))

	if output == "" {
		output = tc.streamingOutput
	}

	if output != "" {
		lines := strings.Split(output, "\n")
		if !expanded && len(lines) > 10 {
			lines = append(lines[:10], "…")
		}
		for _, l := range lines {
			rawLines = append(rawLines, s.Muted().Background(bgColor).Render(l))
		}
	}

	return renderBox(rawLines, chatW, chatW, boxStyle, false)
}

func renderFileAttachment(path, content, extra string, chatW int, s Style, expanded bool) string {
	bgColor := s.ToolRunningBgColor()
	boxStyle := lipgloss.NewStyle().Background(bgColor).PaddingLeft(2).PaddingRight(2)

	var rawLines []string
	header := "📎 file: " + path
	rawLines = append(rawLines, s.ToolCall().Bold(true).Foreground(s.AccentColor()).Background(bgColor).Render(header))

	if expanded {
		for _, l := range strings.Split(content, "\n") {
			rawLines = append(rawLines, s.Muted().Background(bgColor).Render(l))
		}
	} else {
		rawLines = append(rawLines, s.Muted().Background(bgColor).Render(" (Ctrl+O to expand)"))
	}

	res := renderBox(rawLines, chatW, chatW, boxStyle, false)
	if extra != "" {
		res += "\n\n" + extra
	}
	return res
}

func renderSkill(name, args, location, content string, chatW int, s Style, expanded bool) string {
	bgColor := s.ToolRunningBgColor()
	boxStyle := lipgloss.NewStyle().Background(bgColor).PaddingLeft(2).PaddingRight(2)

	var rawLines []string
	header := "✦ skill: " + name
	if args != "" {
		header += " " + args
	}
	rawLines = append(rawLines, s.ToolCall().Bold(true).Foreground(s.AccentColor()).Background(bgColor).Render(header))

	if expanded {
		rawLines = append(rawLines, s.Muted().Background(bgColor).Render("Location: "+location))
		rawLines = append(rawLines, "")
		for _, l := range strings.Split(content, "\n") {
			rawLines = append(rawLines, s.Muted().Background(bgColor).Render(l))
		}
	} else {
		rawLines = append(rawLines, s.Muted().Background(bgColor).Render(" (Ctrl+O to expand)"))
	}

	return renderBox(rawLines, chatW, chatW, boxStyle, false)
}

func renderEntry(e historyEntry, s Style, chatW int, toolCallsExpanded bool) string {
	msgW := int(float64(chatW) * msgWidthRatio)

	var parts []string

	for i, item := range e.items {
		switch item.kind {
		case contentItemThinking:
			parts = append(parts, wrapAndLeftAlign(item.text, chatW, msgW, s.ThinkingBox()))

		case contentItemText:
			var rendered string
			switch e.role {
			case "user":
				// Check for skill block
				if matches := skillRegex.FindStringSubmatch(item.text); len(matches) >= 4 {
					name := matches[1]
					location := matches[2]
					body := matches[3]
					extra := strings.TrimSpace(matches[4])

					parts = append(parts, renderSkill(name, extra, location, body, chatW, s, toolCallsExpanded))
					continue
				}
				// Check for file attachment
				if matches := fileRegex.FindStringSubmatch(item.text); len(matches) >= 4 {
					path := matches[1]
					body := matches[2]
					extra := strings.TrimSpace(matches[3])

					parts = append(parts, renderFileAttachment(path, body, extra, chatW, s, toolCallsExpanded))
					continue
				}
				rendered = wrapAndRightAlign(item.text, chatW, msgW, s.UserBox())
			case "assistant":
				rendered = wrapAndLeftAlign(item.text, chatW, msgW, s.AssistantBox())
			case "error", "warning", "info", "success", "system":
				noticeStyle := s.NoticeBox(e.role).MarginBottom(0)
				wrapped := lipgloss.Wrap(item.text, chatW-4, "")
				lines := strings.Split(wrapped, "\n")
				var noticeLines []string
				for _, line := range lines {
					noticeLines = append(noticeLines, noticeStyle.Width(chatW).Render(line))
				}
				blank := noticeStyle.Width(chatW).Render("")
				rendered = blank + "\n" + strings.Join(noticeLines, "\n") + "\n" + blank
			default:
				rendered = wrapAndLeftAlign(item.text, chatW, msgW, s.AssistantBox())
			}
			parts = append(parts, rendered)

		case contentItemToolCall:
			// Output is always inserted immediately after its tool call.
			var output string
			if i+1 < len(e.items) && e.items[i+1].kind == contentItemToolOutput {
				output = e.items[i+1].out.content
			}
			parts = append(parts, renderToolCall(item.tc, output, chatW, s, toolCallsExpanded))

		case contentItemToolOutput:
			// Rendered inline with its tool call above — skip standalone rendering
			// (output is already embedded in the tool call card)
			_ = item
		}
	}

	rendered := strings.Join(parts, "\n\n")

	// Trailing newline ensures blank-line spacing between entries when joined
	return rendered + "\n"
}

func renderFooter(m *model) string {
	// Left side
	leftParts := make([]string, 0, 4)
	if m.isRunning {
		leftParts = append(leftParts, m.style.StatusWorking().Render("● running"))
	} else if m.isCompacting.Load() {
		leftParts = append(leftParts, m.style.StatusWorking().Render("● compacting"))
	} else {
		leftParts = append(leftParts, m.style.StatusIdle().Render("idle"))
	}
	leftParts = append(leftParts, m.style.Dim().Render("│ "+m.thinking))
	leftParts = append(leftParts, m.style.Muted().Render(m.provider+"/"+m.modelName))

	// Queue display
	if m.queuedSteer > 0 || m.queuedFollowUp > 0 {
		var queueParts []string
		if m.queuedSteer > 0 {
			queueParts = append(queueParts, fmt.Sprintf("%d steer", m.queuedSteer))
		}
		if m.queuedFollowUp > 0 {
			queueParts = append(queueParts, fmt.Sprintf("%d follow-up", m.queuedFollowUp))
		}
		leftParts = append(leftParts, m.style.Dim().Render("│ queued: "+strings.Join(queueParts, ", ")))
	}

	left := strings.Join(leftParts, " ")

	// Right side — progress bar for context usage
	if m.contextWindow <= 0 {
		return left
	}
	right := m.renderProgressBar()

	// Right-align within available width
	avail := m.width - m.style.FooterPaddingX()
	gap := avail - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) renderProgressBar() string {
	pct := float64(m.tokens) / float64(m.contextWindow)
	if pct > 1 {
		pct = 1
	}
	return m.progressBar.ViewAs(pct)
}
