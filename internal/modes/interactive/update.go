package interactive

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"

	"github.com/goppydae/gollm/internal/config"
	"github.com/goppydae/gollm/internal/prompts"
	"github.com/goppydae/gollm/internal/skills"
	"sort"
)

// Update implements tea.Model.Update.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - borderOffset)
		m.vp.SetWidth(msg.Width - borderOffset - chatMargin*2)
		m.input.SetHeight(m.currentInputHeight())
		m.vp.SetHeight(m.vpHeight())
		m.refreshViewport()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		m.input, cmd = m.input.Update(msg)
		m.input.SetHeight(m.currentInputHeight())
		return m, cmd

	case agentEventMsg:
		return m.handleAgentEvent(msg.ev)

	case tea.MouseWheelMsg:
		if m.modal.visible {
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		m.userScrolled = !m.vp.AtBottom()
		return m, cmd

	case spinner.TickMsg:
		if m.isRunning {
			m.spinner, cmd = m.spinner.Update(msg)
			m.chatContent = m.buildChatContent()
			m.vp.SetContent(m.chatContent)
			return m, cmd
		}
		return m, nil

	case stopwatch.TickMsg:
		if m.isRunning {
			m.stopwatch, cmd = m.stopwatch.Update(msg)
			m.chatContent = m.buildChatContent()
			m.vp.SetContent(m.chatContent)
			return m, cmd
		}
		return m, nil

	case stopwatch.ResetMsg:
		m.stopwatch, cmd = m.stopwatch.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		if m.isRunning {
			m.progressBar, cmd = m.progressBar.Update(msg)
			m.chatContent = m.buildChatContent()
			m.vp.SetContent(m.chatContent)
			return m, cmd
		}
		return m, nil

	case initialPromptMsg:
		prompt := m.initialPrompt
		m.initialPrompt = ""
		entry := historyEntry{role: "user", items: []contentItem{{kind: contentItemText, text: prompt}}}
		m.history = append(m.history, entry)
		m.newContext()
		err := m.ag.Prompt(m.ctx, prompt)
		if err != nil {
			m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: err.Error()}}})
		}
		return m, listenForEvent(m.eventCh)

	case stopwatch.StartStopMsg:
		m.stopwatch, cmd = m.stopwatch.Update(msg)
		return m, cmd

	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		var cmd2 tea.Cmd
		m.vp, cmd2 = m.vp.Update(msg)
		return m, tea.Batch(cmd, cmd2)
	}
}

// onResize simulates a WindowSizeMsg — used by tests.
func (m *model) onResize(width, height int) {
	m.width = width
	m.height = height
	m.input.SetWidth(width - borderOffset)
	m.vp.SetWidth(width - borderOffset - chatMargin*2)
	m.input.SetHeight(m.currentInputHeight())
	m.vp.SetHeight(m.vpHeight())
}

// currentInputHeight returns the textarea height needed for the current content.
func (m *model) currentInputHeight() int {
	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines < inputHeight {
		return inputHeight
	}
	// Cap input height to roughly 30% of terminal height
	maxH := m.height / 3
	if maxH < inputHeight {
		maxH = inputHeight
	}
	if lines > maxH {
		return maxH
	}
	return lines
}

// vpHeight returns the correct viewport height given current model state.
func (m *model) vpHeight() int {
	pickerH := 0
	if m.picker.Open {
		if len(m.picker.Matches) == 0 {
			pickerH = 1
		} else {
			pickerH = pickerPageSize
		}
	}
	return m.height - headerHeight - m.currentInputHeight() - footerHeight - separatorHeight - pickerH
}

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()

	// Ctrl+C: abort agent and clear input
	if key.Mod == tea.ModCtrl && key.Code == 'c' {
		m.ag.Abort()
		m.input.SetValue("")
		m.input.SetHeight(inputHeight)
		m.historyIndex = -1
		m.draftInput = ""
		m.vp.SetHeight(m.vpHeight())
		return m, listenForEvent(m.eventCh)
	}

	// Block input if compacting
	if m.isCompacting.Load() {
		return m, nil
	}

	// Modal is open
	if m.modal.visible {
		return m.handleModalKey(msg)
	}

	// Picker is open
	if m.picker.Open {
		return m.handlePickerKey(msg)
	}

	// Escape: close modal, abort running agent, or close picker
	if key.Code == tea.KeyEscape {
		if m.modal.visible {
			m.modal.close()
			return m, listenForEvent(m.eventCh)
		}
		if m.picker.Open {
			m.picker.Close()
			m.vp.SetHeight(m.vpHeight())
			return m, listenForEvent(m.eventCh)
		}
		if m.isRunning {
			m.ag.Abort()
			m.cancel() // cancel context to unblock LLM stream
			return m, listenForEvent(m.eventCh)
		}
		return m, nil
	}

	// Up: cycle back in history
	if Matches(msg, K.Up()) {
		// Only trigger history if we are on the first line
		if m.input.Line() > 0 {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if len(m.promptHistory) == 0 {
			return m, nil
		}
		if m.historyIndex == -1 {
			m.draftInput = m.input.Value()
			m.historyIndex = len(m.promptHistory) - 1
		} else if m.historyIndex > 0 {
			m.historyIndex--
		} else {
			return m, nil
		}
		m.input.SetValue(m.promptHistory[m.historyIndex])
		m.input.SetHeight(m.currentInputHeight())
		m.vp.SetHeight(m.vpHeight())
		return m, nil
	}

	// Down: cycle forward in history
	if Matches(msg, K.Down()) {
		// Only trigger history if we are on the last line
		if m.input.Line() < m.input.LineCount()-1 {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.historyIndex == -1 {
			return m, nil
		}
		if m.historyIndex < len(m.promptHistory)-1 {
			m.historyIndex++
			m.input.SetValue(m.promptHistory[m.historyIndex])
		} else {
			m.historyIndex = -1
			m.input.SetValue(m.draftInput)
		}
		m.input.SetHeight(m.currentInputHeight())
		m.vp.SetHeight(m.vpHeight())
		return m, nil
	}

	// Shift+Enter: insert newline
	if Matches(msg, K.Shift("enter")) {
		m.input.InsertString("\n")
		m.input.SetHeight(m.currentInputHeight())
		m.vp.SetHeight(m.vpHeight())
		return m, nil
	}

	// Ctrl+Enter: queue follow-up message
	if Matches(msg, K.Ctrl("enter")) {
		if m.input.Value() == "" {
			return m, nil
		}
		raw := m.input.Value()

		// Add to prompt history
		if raw != "" && (len(m.promptHistory) == 0 || m.promptHistory[len(m.promptHistory)-1] != raw) {
			m.promptHistory = append(m.promptHistory, raw)
		}
		m.historyIndex = -1
		m.draftInput = ""

		m.input.SetValue("")
		m.input.SetHeight(inputHeight)
		m.userScrolled = false
		m.vp.GotoBottom()
		m.vp.SetHeight(m.vpHeight())

		expanded := expandAttachments(raw)
		entry := historyEntry{role: "user", items: []contentItem{{kind: contentItemText, text: raw}}}
		if m.isRunning && len(m.history) > 0 && m.history[len(m.history)-1].role == "assistant" {
			idx := len(m.history) - 1
			m.history = append(m.history[:idx+1], m.history[idx])
			m.history[idx] = entry
		} else {
			m.history = append(m.history, entry)
		}
		if m.isRunning {
			m.ag.FollowUp(expanded)
		} else {
			m.newContext()
			err := m.ag.Prompt(m.ctx, expanded)
			if err != nil {
				m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: err.Error()}}})
			}
		}
		return m, listenForEvent(m.eventCh)
	}

	// Enter: send message or process slash command (steer if running)
	if key.Code == tea.KeyEnter && key.Mod == 0 {
		if m.input.Value() == "" {
			return m, nil
		}
		raw := m.input.Value()

		// Add to prompt history
		if raw != "" && (len(m.promptHistory) == 0 || m.promptHistory[len(m.promptHistory)-1] != raw) {
			m.promptHistory = append(m.promptHistory, raw)
		}
		m.historyIndex = -1
		m.draftInput = ""

		m.input.SetValue("")
		m.input.SetHeight(inputHeight)
		m.userScrolled = false
		m.vp.GotoBottom()
		m.vp.SetHeight(m.vpHeight())

		// Check for slash command
		if cmd := parseSlashCommand(raw); cmd != nil && knownCommand(cmd.name) {
			if m.isRunning && (cmd.name == "new" || cmd.name == "resume" || cmd.name == "import" || cmd.name == "tree") {
				m.history = append(m.history, historyEntry{role: "warning", items: []contentItem{{kind: contentItemText, text: "Cannot change session while agent is running. Abort first with Esc."}}})
				return m.refreshViewport(), listenForEvent(m.eventCh)
			}
			result, err := handleSlashCommand(cmd, m.sessionMgr, m.ag, m.config)
			if err != nil {
				m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: err.Error()}}})
			} else if result != nil {
				if result.syncHistory {
					m.syncHistoryFromAgent()
				}
				if len(result.historyEntry.items) > 0 {
					m.history = append(m.history, result.historyEntry)
				}
				if result.modalKind != modalNone {
					switch result.modalKind {
					case modalStats:
						stats := m.ag.GetStats()
						stats.SessionFile = m.sessionMgr.SessionPath(stats.SessionID)
						m.modal.openStatsModal(stats, m.style)
					case modalConfig:
						m.modal.openConfigModal(m.modelName, m.provider, m.thinking, m.config.Theme, "interactive",
							m.config.OllamaBaseURL, m.config.OpenAIBaseURL, "...", m.config.LlamaCppBaseURL,
							m.config.Compaction.Enabled, m.config.Compaction.ReserveTokens, m.config.Compaction.KeepRecentTokens, m.style)
					case modalTree:
						if len(result.modalNodes) > 0 {
							m.modal.openTreeModal(result.modalNodes, m.ag.GetSession().ID, m.style)
						} else {
							m.openModal(modalTree)
						}
					default:
						m.openModal(result.modalKind)
					}
				}
				if result.compact {
					return m, tea.Batch(
						func() tea.Msg {
							m.ag.Compact(m.config.Compaction.KeepRecentTokens)
							return nil
						},
						listenForEvent(m.eventCh),
					)
				}
				if result.quit {
					return m, tea.Quit
				}
				// expandInput: replace editor content (e.g. /prompt:name)
				if result.expandInput != "" {
					m.input.SetValue(result.expandInput)
					m.input.SetHeight(m.currentInputHeight())
				}

				// sendDirectly: send text to agent immediately
				if result.sendDirectly != "" {
					m.userScrolled = false
					m.vp.GotoBottom()
					m.vp.SetHeight(m.vpHeight())

					entry := historyEntry{role: "user", items: []contentItem{{kind: contentItemText, text: result.sendDirectly}}}
					m.history = append(m.history, entry)

					if m.isRunning {
						m.ag.Steer(result.sendDirectly)
					} else {
						m.newContext()
						err := m.ag.Prompt(m.ctx, result.sendDirectly)
						if err != nil {
							m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: err.Error()}}})
						}
					}
				}

				// invokeTool: manually trigger a tool call
				if result.invokeTool != "" {
					m.userScrolled = false
					m.vp.GotoBottom()
					m.vp.SetHeight(m.vpHeight())

					err := m.ag.InvokeTool(context.Background(), result.invokeTool, result.invokeToolArgs)
					if err != nil {
						entry := historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Tool invocation failed: %v", err)}}}
						m.history = append(m.history, entry)
					}
					return m, nil
				}
			}
			return m.refreshViewport(), listenForEvent(m.eventCh)
		}

		// Intercept !command / !!command inline bash
		if strings.HasPrefix(raw, "!") {
			bangResult, sendDirectly := HandleBangCommand(raw)
			if sendDirectly {
				// !! → send output directly to model
				entry := historyEntry{role: "user", items: []contentItem{{kind: contentItemText, text: raw}}}
				m.history = append(m.history, entry)
				m.newContext()
				if err := m.ag.Prompt(m.ctx, bangResult.Output); err != nil {
					m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: err.Error()}}})
				}
			} else {
				// ! → paste output into editor
				m.input.SetValue(bangResult.Output)
				m.input.SetHeight(m.currentInputHeight())
			}
			return m.refreshViewport(), listenForEvent(m.eventCh)
		}

		expanded := expandAttachments(raw)
		entry := historyEntry{role: "user", items: []contentItem{{kind: contentItemText, text: raw}}}
		if m.isRunning && len(m.history) > 0 && m.history[len(m.history)-1].role == "assistant" {
			idx := len(m.history) - 1
			m.history = append(m.history[:idx+1], m.history[idx])
			m.history[idx] = entry
		} else {
			m.history = append(m.history, entry)
		}
		if m.isRunning {
			m.ag.Steer(expanded)
		} else {
			m.newContext()
			err := m.ag.Prompt(m.ctx, expanded)
			if err != nil {
				m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: err.Error()}}})
				m.isRunning = false
			}
		}
		return m.refreshViewport(), listenForEvent(m.eventCh)
	}

	// Ctrl+O: toggle all tool call expansion
	if Matches(msg, K.Ctrl("o")) {
		m.toolCallsExpanded = !m.toolCallsExpanded
		m.chatContent = m.buildChatContent()
		m.vp.SetContent(m.chatContent)
		return m, listenForEvent(m.eventCh)
	}

	// Ctrl+P: cycle to the next model in the --models list
	if Matches(msg, K.Ctrl("p")) && len(m.models) > 0 {
		m.modelIndex = (m.modelIndex + 1) % len(m.models)
		next := strings.TrimSpace(m.models[m.modelIndex])
		// Parse optional provider prefix
		if idx := strings.IndexByte(next, '/'); idx >= 0 {
			m.provider = next[:idx]
			m.modelName = next[idx+1:]
		} else {
			m.modelName = next
		}
		m.config.Provider = m.provider
		m.config.Model = m.modelName
		newProv, provErr := config.BuildProvider(m.config)
		if provErr != nil {
			m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: provErr.Error()}}})
			return m.refreshViewport(), listenForEvent(m.eventCh)
		}
		m.ag.SetProvider(newProv)
		notice := fmt.Sprintf("Switched model → %s", next)
		m.history = append(m.history, historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: notice}}})
		return m.refreshViewport(), listenForEvent(m.eventCh)
	}

	// Viewport scroll keys
	if key.Code == tea.KeyUp || key.Code == tea.KeyDown ||
		key.Code == tea.KeyPgUp || key.Code == tea.KeyPgDown {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		m.userScrolled = !m.vp.AtBottom()
		return m, cmd
	}

	// Normal input
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.input.SetHeight(m.currentInputHeight())
	m = m.updatePicker()
	m.vp.SetHeight(m.vpHeight())
	return m, cmd
}

func (m *model) handlePickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.picker.Update(msg) {
		return m, nil
	}

	key := msg.Key()
	switch key.Code {
	case tea.KeyEscape:
		m.picker.Close()
		m.vp.SetHeight(m.vpHeight())

	case tea.KeyEnter, tea.KeyTab:
		selected, ok := m.picker.Selected()
		if ok {
			switch m.picker.Kind {
			case pickerTypeSlash:
				m.input.SetValue("/" + selected + " ")
				m.picker.Close()
				m.vp.SetHeight(m.vpHeight())
				m = m.updatePicker()
				m.vp.SetHeight(m.vpHeight())
				return m, nil
			case pickerTypeSession:
				// Items are formatted as "ID | ..."
				parts := strings.Split(selected, " | ")
				id := parts[0]
				m.input.SetValue("/resume " + id)
			case pickerTypeSkill:
				prefix := "skill:"
				if strings.Contains(m.input.Value(), "/skill ") {
					prefix = "skill "
				}
				m.input.SetValue("/" + prefix + selected + " ")
			case pickerTypePrompt:
				prefix := "prompt:"
				if strings.Contains(m.input.Value(), "/prompt ") {
					prefix = "prompt "
				}
				m.input.SetValue("/" + prefix + selected + " ")
			default:
				if _, atIdx, ok := atFragment(m.input.Value()); ok {
					m.input.SetValue(replaceAtFragment(m.input.Value(), atIdx, selected+" "))
				}
			}
		}
		m.picker.Close()
		m.vp.SetHeight(m.vpHeight())

	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m = m.updatePicker()
		m.vp.SetHeight(m.vpHeight())
		return m, cmd
	}
	return m, nil
}

func (m *model) handleModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()
	switch key.Code {
	case tea.KeyEscape:
		m.modal.close()
		return m, listenForEvent(m.eventCh)

	case tea.KeyUp:
		if m.modal.kind == modalTree && m.modal.cursor > 0 {
			m.modal.cursor--
			if m.modal.cursor < m.modal.offset {
				m.modal.offset = m.modal.cursor
			}
			m.modal.refreshTreeContent(m.ag.GetSession().ID, m.style)
		}
		return m, nil

	case tea.KeyDown:
		if m.modal.kind == modalTree && m.modal.cursor < len(m.modal.nodes)-1 {
			m.modal.cursor++
			if m.modal.cursor >= m.modal.offset+treePageSize {
				m.modal.offset = m.modal.cursor - treePageSize + 1
			}
			m.modal.refreshTreeContent(m.ag.GetSession().ID, m.style)
		}
		return m, nil

	case tea.KeyEnter:
		if m.modal.kind == modalTree && len(m.modal.nodes) > 0 {
			selected := m.modal.nodes[m.modal.cursor].Node.ID
			m.modal.close()

			// Load and resume session correctly
			sess, err := m.sessionMgr.Load(selected)
			if err != nil {
				m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Failed to load session %s: %v", selected, err)}}})
				return m.refreshViewport(), listenForEvent(m.eventCh)
			}

			m.ag.LoadSession(sess.ToTypes())
			m.syncHistoryFromAgent()
			m.history = append(m.history, historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Switched to session: %s", selected)}}})
			return m.refreshViewport(), listenForEvent(m.eventCh)
		}

	default:
		key := msg.Key()
		if m.modal.kind == modalTree && key.Code == 'b' && len(m.modal.nodes) > 0 {
			selected := m.modal.nodes[m.modal.cursor].Node.ID
			m.modal.close()

			// Load source session
			source, err := m.sessionMgr.Load(selected)
			if err != nil {
				m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Failed to load session %s: %v", selected, err)}}})
				return m.refreshViewport(), listenForEvent(m.eventCh)
			}

			// Fork session
			forked, err := m.sessionMgr.Fork(source)
			if err != nil {
				m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Failed to fork session: %v", err)}}})
				return m.refreshViewport(), listenForEvent(m.eventCh)
			}

			// Load new fork
			m.ag.LoadSession(forked.ToTypes())
			m.syncHistoryFromAgent()
			m.history = append(m.history, historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("Branched from session: %s", selected)}}})
			m.history = append(m.history, historyEntry{role: "info", items: []contentItem{{kind: contentItemText, text: fmt.Sprintf("New session created: %s", forked.ID)}}})
			
			return m.refreshViewport(), listenForEvent(m.eventCh)
		}
	}
	return m, nil
}

func (m *model) updatePicker() *model {
	val := m.input.Value()

	// 1. Session picker (/resume )
	if strings.HasPrefix(val, "/resume ") {
		query := val[len("/resume "):]
		summaries, _ := m.sessionMgr.ListSummaries()
		
		var items []string
		for _, s := range summaries {
			firstMsg := s.FirstMessage
			if len(firstMsg) > 40 {
				firstMsg = firstMsg[:37] + "..."
			}
			firstMsg = strings.ReplaceAll(firstMsg, "\n", " ")
			
			items = append(items, fmt.Sprintf("%s | %-40s | C: %s | U: %s",
				s.ID,
				firstMsg,
				s.CreatedAt.Format("Jan 02 15:04"),
				s.UpdatedAt.Format("Jan 02 15:04")))
		}
		
		m.picker.Reset(pickerTypeSession, query, items)
		return m
	}

	// 2. Skill picker (/skill: or /skill )
	if strings.HasPrefix(val, "/skill:") || strings.HasPrefix(val, "/skill ") {
		prefix := "/skill:"
		if strings.HasPrefix(val, "/skill ") {
			prefix = "/skill "
		}
		query := val[len(prefix):]
		found, _ := skills.Discover(m.config.SkillPaths...)
		var names []string
		for _, s := range found {
			names = append(names, s.Name)
		}
		m.picker.Reset(pickerTypeSkill, query, names)
		return m
	}

	// 3. Prompt picker (/prompt: or /prompt )
	if strings.HasPrefix(val, "/prompt:") || strings.HasPrefix(val, "/prompt ") {
		prefix := "/prompt:"
		if strings.HasPrefix(val, "/prompt ") {
			prefix = "/prompt "
		}
		query := val[len(prefix):]
		found, _ := prompts.Discover(m.config.PromptTemplatePaths...)
		var names []string
		for _, p := range found {
			names = append(names, strings.TrimSuffix(filepath.Base(p.Path), ".md"))
		}
		m.picker.Reset(pickerTypePrompt, query, names)
		return m
	}

	// 4. Slash command picker
	if strings.HasPrefix(val, "/") && !strings.ContainsRune(val, ' ') {
		query := val[1:]
		var cmds []string
		cmds = append(cmds, BaseSlashCommands...)

		// Add skills
		skillDirs := append(skills.DefaultDirs(), m.config.SkillPaths...)
		foundSkills, _ := skills.Discover(skillDirs...)
		for _, s := range foundSkills {
			cmds = append(cmds, "skill:"+s.Name)
		}

		// Add prompts
		promptDirs := append(prompts.DefaultDirs(), m.config.PromptTemplatePaths...)
		foundPrompts, _ := prompts.Discover(promptDirs...)
		for _, p := range foundPrompts {
			name := strings.TrimSuffix(filepath.Base(p.Path), ".md")
			cmds = append(cmds, "prompt:"+name)
		}

		sort.Strings(cmds)
		
		m.picker.Reset(pickerTypeSlash, query, cmds)
		return m
	}

	// 5. Fallback to file picker (@ trigger)
	query, _, ok := atFragment(val)
	if !ok {
		m.picker.Close()
		return m
	}
	
	if m.fileCache == nil {
		m.fileCache = discoverFiles(".")
	}
	
	m.picker.Reset(pickerTypeFile, query, m.fileCache)
	return m
}

func (m *model) openModal(kind modalKind) {
	m.modal.kind = kind
	m.modal.visible = true
}
