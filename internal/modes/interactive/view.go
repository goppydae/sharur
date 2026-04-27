package interactive

import (
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	glamour "charm.land/glamour/v2"
	lipgloss "charm.land/lipgloss/v2"
)

// hexToTrueColorBg converts a "#rrggbb" hex color to an ANSI true-color
// background escape sequence (\x1b[48;2;R;G;Bm).
func hexToTrueColorBg(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return "\x1b[48;2;45;45;45m" // fallback: #2d2d2d
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

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
	if m.pickerOpen {
		sections = append(sections, m.picker.View())
	}
	sections = append(sections, input, separator(), footerText)
	mainStr := lipgloss.JoinVertical(lipgloss.Top, sections...)

	// Render modal overlay on top using Compositor
	if m.modal.visible {
		modalStr := m.modal.render(m.width, m.height, m.style, m.helper, m.keys)
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

	isCompacting := m.isCompacting.Load()
	if m.isRunning || isCompacting {
		status := "Working..."
		if isCompacting {
			status = "Compacting..."
		}
		elapsed := m.stopwatch.View()
		lines = append(lines, m.style.WorkingIndicator().PaddingLeft(1).Render(m.spinner.View()+" "+status+" "+elapsed))
	}

	return strings.Join(lines, "\n")
}

var (
	mdRenderer      *glamour.TermRenderer
	mdCachedWidth   int
	mdCachedBg      string
	mdCachedCodeBg  string
	mdMu            sync.Mutex
)

func renderMarkdown(content string, width int, bgHex, codeBgHex string) string {
	mdMu.Lock()
	defer mdMu.Unlock()

	if mdRenderer == nil || mdCachedWidth != width || mdCachedBg != bgHex || mdCachedCodeBg != codeBgHex {
		chromaBg := fmt.Sprintf(`{"background_color": "%s"}`, codeBgHex)
		styleJSON := fmt.Sprintf(`{
		"document": {"background_color": "%s", "margin": 0},
		"code_block": {
			"background_color": "%s",
			"margin": 0,
			"padding": 1,
			"chroma": {
				"text":                  `+chromaBg+`,
				"error":                 `+chromaBg+`,
				"comment":               `+chromaBg+`,
				"comment_preproc":       `+chromaBg+`,
				"keyword":               `+chromaBg+`,
				"keyword_reserved":      `+chromaBg+`,
				"keyword_namespace":     `+chromaBg+`,
				"keyword_type":          `+chromaBg+`,
				"operator":              `+chromaBg+`,
				"punctuation":           `+chromaBg+`,
				"name":                  `+chromaBg+`,
				"name_builtin":          `+chromaBg+`,
				"name_tag":              `+chromaBg+`,
				"name_attribute":        `+chromaBg+`,
				"name_class":            `+chromaBg+`,
				"name_constant":         `+chromaBg+`,
				"name_decorator":        `+chromaBg+`,
				"name_exception":        `+chromaBg+`,
				"name_function":         `+chromaBg+`,
				"name_other":            `+chromaBg+`,
				"literal":               `+chromaBg+`,
				"literal_number":        `+chromaBg+`,
				"literal_date":          `+chromaBg+`,
				"literal_string":        `+chromaBg+`,
				"literal_string_escape": `+chromaBg+`,
				"generic_deleted":       `+chromaBg+`,
				"generic_emph":          `+chromaBg+`,
				"generic_inserted":      `+chromaBg+`,
				"generic_strong":        `+chromaBg+`,
				"generic_subheading":    `+chromaBg+`,
				"background":            `+chromaBg+`
			}
		},
		"code": {
			"background_color": "%s"
		}
	}`, bgHex, codeBgHex, codeBgHex)
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(0),
			glamour.WithStylesFromJSONBytes([]byte(styleJSON)),
			glamour.WithChromaFormatter("terminal16m"),
		)
		if err != nil {
			return content
		}
		mdRenderer = r
		mdCachedWidth = width
		mdCachedBg = bgHex
		mdCachedCodeBg = codeBgHex
	}

	out, err := mdRenderer.Render(content)
	if err != nil {
		return content
	}

	return strings.TrimSpace(fixCodeBlockLines(out, codeBgHex))
}

// fixCodeBlockLines finds groups of Chroma-rendered code-block lines
// (identified by the true-colour bg escape for codeBgHex), strips all ANSI
// reset sequences so the background stays active, and pads every line in
// the group — including blank source lines — to the width of the longest
// line. This makes the code-block background a solid, uniform rectangle.
//
// With glamour's WordWrap disabled (0), the PaddingWriter adds no
// document-background suffix spaces, so the only resets in code-block
// lines are Chroma's own \x1b[0m and IndentWriter's \x1b[m; both are
// stripped here before measuring and padding.
func fixCodeBlockLines(rendered, codeBgHex string) string {
	bgANSI := hexToTrueColorBg(codeBgHex)
	const reset = "\x1b[0m"
	const resetShort = "\x1b[m"

	stripResets := func(s string) string {
		s = strings.ReplaceAll(s, reset, "")
		s = strings.ReplaceAll(s, resetShort, "")
		return s
	}

	lines := strings.Split(rendered, "\n")

	for i := 0; i < len(lines); {
		if !strings.Contains(lines[i], bgANSI) {
			i++
			continue
		}

		// Collect contiguous code-block lines plus any intervening blank
		// lines (visible width 0) that sit between bgANSI lines.
		start := i
		i++
		for i < len(lines) {
			if strings.Contains(lines[i], bgANSI) {
				i++
				continue
			}
			// Include visually empty lines that are sandwiched between
			// code-block lines (e.g. blank source lines without explicit bg).
			if lipgloss.Width(lines[i]) == 0 && i+1 < len(lines) && strings.Contains(lines[i+1], bgANSI) {
				i++
				continue
			}
			break
		}
		end := i

		// Strip all resets; terminal16m re-emits full fg+bg per token so
		// there is no colour bleed between tokens after stripping.
		stripped := make([]string, end-start)
		maxW := 0
		for j, line := range lines[start:end] {
			s := stripResets(line)
			stripped[j] = s
			if w := lipgloss.Width(s); w > maxW {
				maxW = w
			}
		}

		// Pad every line to maxW. Spaces appended after the last Chroma
		// fg+bg sequence inherit the #2d2d2d background automatically.
		// Blank lines get an explicit bgANSI prefix so the inherited bg is
		// always the code background.
		for j, s := range stripped {
			if !strings.Contains(s, bgANSI) {
				s = bgANSI + s
			}
			pad := maxW - lipgloss.Width(s)
			if pad < 0 {
				pad = 0
			}
			lines[start+j] = s + strings.Repeat(" ", pad) + reset
		}
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

	argLines := strings.Split(tc.arg, "\n")
	header := icon + " " + tc.name
	if len(argLines) > 0 && argLines[0] != "" {
		header += " " + argLines[0]
	}
	rawLines = append(rawLines, s.ToolCall().Bold(true).Background(bgColor).Render(header))

	if len(argLines) > 1 {
		for _, l := range argLines[1:] {
			rawLines = append(rawLines, s.Muted().Background(bgColor).Render(l))
		}
	}

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
	header := "skill: " + name
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
				bgHex := s.AssistantBgHex()
				rendered = wrapAndLeftAlign(renderMarkdown(item.text, msgW-4, bgHex, s.CodeBgHex()), chatW, msgW, s.AssistantBox())
			case "error", "warning", "info", "success", "system":
				noticeStyle := s.NoticeBox(e.role).MarginBottom(0)
				text := Capitalize(item.text)
				wrapped := lipgloss.Wrap(text, chatW-4, "")
				lines := strings.Split(wrapped, "\n")
				var noticeLines []string
				for _, line := range lines {
					noticeLines = append(noticeLines, noticeStyle.Width(chatW).Render(line))
				}
				blank := noticeStyle.Width(chatW).Render("")
				rendered = blank + "\n" + strings.Join(noticeLines, "\n") + "\n" + blank
			default:
				bgHex := s.AssistantBgHex()
				rendered = wrapAndLeftAlign(renderMarkdown(item.text, msgW-4, bgHex, s.CodeBgHex()), chatW, msgW, s.AssistantBox())
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
	isCompacting := m.isCompacting.Load()
	if isCompacting {
		leftParts = append(leftParts, m.style.StatusWorking().Render("● Compacting"))
	} else if m.isRunning {
		leftParts = append(leftParts, m.style.StatusWorking().Render("● Running"))
	} else {
		leftParts = append(leftParts, m.style.StatusIdle().Render("Idle"))
	}

	if m.isRunning || isCompacting {
		leftParts = append(leftParts, m.stopwatch.View())
	}
	if m.thinking != "" && m.thinking != "none" {
		leftParts = append(leftParts, m.style.Dim().Render("│"))
		leftParts = append(leftParts, m.style.Dim().Render(m.thinking))
	}
	leftParts = append(leftParts, m.style.Dim().Render("│"))
	leftParts = append(leftParts, m.style.Muted().Render(m.provider+"/"+m.modelName))

	// Queue display
	if m.queuedSteer > 0 || m.queuedFollowUp > 0 {
		var queueParts []string
		if m.queuedSteer > 0 {
			queueParts = append(queueParts, fmt.Sprintf("%d Steer", m.queuedSteer))
		}
		if m.queuedFollowUp > 0 {
			queueParts = append(queueParts, fmt.Sprintf("%d Follow-up", m.queuedFollowUp))
		}
		leftParts = append(leftParts, m.style.Dim().Render("│ Queued: "+strings.Join(queueParts, ", ")))
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
