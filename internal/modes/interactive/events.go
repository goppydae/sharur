package interactive

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
)

func (m *model) handleAgentEvent(ev *pb.AgentEvent) tea.Cmd {
	var cmds []tea.Cmd

	switch p := ev.Payload.(type) {
	case *pb.AgentEvent_CompactStart:
		_ = p
		m.isCompacting.Store(true)
		cmds = append(cmds, m.spinner.Tick)
		cmds = append(cmds, m.stopwatch.Reset())
		cmds = append(cmds, m.stopwatch.Start())

	case *pb.AgentEvent_CompactEnd:
		_ = p
		m.isCompacting.Store(false)
		if !m.isRunning {
			cmds = append(cmds, m.stopwatch.Stop())
		}
		cmds = append(cmds, m.syncHistoryCmd())
		cmds = append(cmds, m.syncStateCmd())

	case *pb.AgentEvent_QueueUpdate:
		_ = p
		cmds = append(cmds, m.syncStateCmd())
	case *pb.AgentEvent_AgentStart:
		_ = p
		m.isRunning = true
		m.startTime = time.Now()
		cmds = append(cmds, m.spinner.Tick)
		cmds = append(cmds, m.stopwatch.Reset())
		cmds = append(cmds, m.stopwatch.Start())
		cmds = append(cmds, m.progressBar.SetPercent(0))

	case *pb.AgentEvent_TextDelta:
		entry := m.ensureAssistantEntry()
		if len(entry.items) > 0 && entry.items[len(entry.items)-1].kind == contentItemText {
			entry.items[len(entry.items)-1].text += p.TextDelta.Content
		} else {
			entry.items = append(entry.items, contentItem{kind: contentItemText, text: p.TextDelta.Content})
		}
		m.tokens += (len(p.TextDelta.Content) + 3) / 4

	case *pb.AgentEvent_ToolCall:
		tc := p.ToolCall
		duplicate := false
		if tc.Id != "" {
			for hIdx := len(m.history) - 1; hIdx >= 0; hIdx-- {
				if m.history[hIdx].role != "assistant" {
					break
				}
				for _, item := range m.history[hIdx].items {
					if item.kind == contentItemToolCall && item.tc.id == tc.Id {
						duplicate = true
						break
					}
				}
				if duplicate {
					break
				}
			}
		}
		if !duplicate {
			entry := m.ensureAssistantEntry()
			arg := extractFirstArgument(tc.Name, tc.ArgsJson)
			entry.items = append(entry.items, contentItem{
				kind: contentItemToolCall,
				tc: toolCallEntry{
					id:     tc.Id,
					name:   tc.Name,
					arg:    arg,
					status: toolCallRunning,
				},
			})
		}

	case *pb.AgentEvent_ToolDelta:
		td := p.ToolDelta
		if td.Content != "" {
			for hIdx := len(m.history) - 1; hIdx >= 0; hIdx-- {
				if m.history[hIdx].role != "assistant" {
					continue
				}
				entry := &m.history[hIdx]
				for i := range entry.items {
					if entry.items[i].kind == contentItemToolCall && entry.items[i].tc.id == td.ToolCallId {
						if entry.items[i].tc.status == toolCallRunning {
							entry.items[i].tc.streamingOutput += td.Content
						}
						break
					}
				}
			}
		}

	case *pb.AgentEvent_ThinkingDelta:
		if p.ThinkingDelta.Content != "" {
			entry := m.ensureAssistantEntry()
			if len(entry.items) > 0 && entry.items[len(entry.items)-1].kind == contentItemThinking {
				entry.items[len(entry.items)-1].text += p.ThinkingDelta.Content
			} else {
				entry.items = append(entry.items, contentItem{kind: contentItemThinking, text: p.ThinkingDelta.Content})
			}
			m.tokens += (len(p.ThinkingDelta.Content) + 3) / 4
		}

	case *pb.AgentEvent_ToolOutput:
		to := p.ToolOutput
		var entry *historyEntry
		found := false
		for hIdx := len(m.history) - 1; hIdx >= 0; hIdx-- {
			if m.history[hIdx].role != "assistant" {
				continue
			}
			entry = &m.history[hIdx]
			for i := range entry.items {
				if entry.items[i].kind == contentItemToolCall && entry.items[i].tc.id == to.ToolCallId {
					if to.ToolCallId == "" && entry.items[i].tc.status != toolCallRunning {
						continue
					}
					entry.items[i].tc.status = toolCallSuccess
					if to.IsError || strings.HasPrefix(to.Content, "Error:") || strings.HasPrefix(to.Content, "tool error:") {
						entry.items[i].tc.status = toolCallFailure
					}
					outItem := contentItem{
						kind: contentItemToolOutput,
						out: toolOutputEntry{
							toolCallID: to.ToolCallId,
							toolName:   to.ToolName,
							content:    to.Content,
							isError:    to.IsError,
						},
					}
					entry.items = append(entry.items, contentItem{})
					copy(entry.items[i+2:], entry.items[i+1:])
					entry.items[i+1] = outItem
					found = true
					break
				}
			}
			if found {
				break
			}
		}

	case *pb.AgentEvent_MessageEnd:
		m.newAssistantEntry = true
		if p.MessageEnd.TotalTokens > 0 {
			m.tokens = int(p.MessageEnd.TotalTokens)
		}

	case *pb.AgentEvent_StateChange:
		sc := p.StateChange
		m.isRunning = (sc.To == "thinking" || sc.To == "executing" || sc.To == "compacting")
		if m.isRunning {
			cmds = append(cmds, m.spinner.Tick)
			cmds = append(cmds, m.stopwatch.Start())
		}

	case *pb.AgentEvent_AgentEnd:
		_ = p
		m.isRunning = false
		cmds = append(cmds, m.stopwatch.Stop())
		cmds = append(cmds, m.stopwatch.Reset())

	case *pb.AgentEvent_Abort:
		_ = p
		m.isRunning = false
		cmds = append(cmds, m.stopwatch.Stop())
		cmds = append(cmds, m.stopwatch.Reset())

	case *pb.AgentEvent_Error:
		m.isRunning = false
		cmds = append(cmds, m.stopwatch.Stop())
		cmds = append(cmds, m.stopwatch.Reset())
		m.history = append(m.history, historyEntry{role: "error", items: []contentItem{{kind: contentItemText, text: p.Error.Message}}})

	case *pb.AgentEvent_Tokens:
		m.tokens = int(p.Tokens.Value)
	}

	m.refreshViewport()
	return tea.Batch(cmds...)
}

// syncHistoryCmd returns a command to fetch the latest conversation history.
func (m *model) syncHistoryCmd() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetMessages(context.Background(), &pb.GetMessagesRequest{SessionId: m.sessionID})
		if err != nil {
			return syncHistoryMsg{err: err}
		}
		return syncHistoryMsg{messages: resp.Messages}
	}
}

// syncStateCmd returns a command to fetch the latest session state (model, thinking level, etc.).
func (m *model) syncStateCmd() tea.Cmd {
	return func() tea.Msg {
		state, err := m.client.GetState(context.Background(), &pb.GetStateRequest{SessionId: m.sessionID})
		if err != nil {
			return syncStateMsg{err: err}
		}
		return syncStateMsg{state: state}
	}
}

// compactCmd returns a command to trigger a context compaction.
func (m *model) compactCmd() tea.Cmd {
	return func() tea.Msg {
		_, err := m.client.Compact(context.Background(), &pb.CompactRequest{SessionId: m.sessionID})
		return compactDoneMsg{err: err}
	}
}

// applyHistorySync updates the model with freshly fetched messages.
func (m *model) applyHistorySync(msgs []*pb.ConversationMessage) {
	var trailingMeta []historyEntry
	for i := len(m.history) - 1; i >= 0; i-- {
		entry := m.history[i]
		// Preserve trailing assistant messages if running, or any notice boxes
		isNotice := entry.role == "info" || entry.role == "success" || entry.role == "warning" || entry.role == "error" || entry.role == "system"
		if (m.isRunning && entry.role == "assistant") || isNotice {
			trailingMeta = append([]historyEntry{entry}, trailingMeta...)
			continue
		}
		break
	}

	m.history = make([]historyEntry, 0, len(msgs)+len(trailingMeta))

	for _, msg := range msgs {
		if msg.Role == "tool" {
			found := false
			for hIdx := len(m.history) - 1; hIdx >= 0; hIdx-- {
				if m.history[hIdx].role != "assistant" {
					continue
				}
				entry := &m.history[hIdx]
				for i := range entry.items {
					if entry.items[i].kind == contentItemToolCall && entry.items[i].tc.id == msg.ToolCallId {
						entry.items[i].tc.status = toolCallSuccess
						if strings.HasPrefix(msg.Content, "Error:") || strings.HasPrefix(msg.Content, "tool error:") {
							entry.items[i].tc.status = toolCallFailure
						}
						outItem := contentItem{
							kind: contentItemToolOutput,
							out: toolOutputEntry{
								toolCallID: msg.ToolCallId,
								content:    msg.Content,
							},
						}
						entry.items = append(entry.items, contentItem{})
						copy(entry.items[i+2:], entry.items[i+1:])
						entry.items[i+1] = outItem
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			continue
		}

		entry := historyEntry{role: msg.Role}

		if msg.Role == "assistant" {
			if msg.Thinking != "" {
				entry.items = append(entry.items, contentItem{kind: contentItemThinking, text: msg.Thinking})
			}
			if msg.Content != "" {
				entry.items = append(entry.items, contentItem{kind: contentItemText, text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				arg := extractFirstArgument(tc.Name, tc.ArgsJson)
				entry.items = append(entry.items, contentItem{
					kind: contentItemToolCall,
					tc: toolCallEntry{
						id:     tc.Id,
						name:   tc.Name,
						arg:    arg,
						status: toolCallRunning,
					},
				})
			}
		} else {
			entry.items = append(entry.items, contentItem{kind: contentItemText, text: msg.Content})
		}

		m.history = append(m.history, entry)
	}
	m.history = append(m.history, trailingMeta...)
	m.newAssistantEntry = true

	m.updatePromptHistory(msgs)
	m.refreshViewport()
}

// syncHistoryFromService remains as a synchronous helper for initial startup or simple cases,
// but should be avoided in the main loop to prevent UI freezes.
func (m *model) syncHistoryFromService() {
	resp, err := m.client.GetMessages(context.Background(), &pb.GetMessagesRequest{SessionId: m.sessionID})
	if err != nil {
		return
	}
	m.applyHistorySync(resp.Messages)

	if state, err := m.client.GetState(context.Background(), &pb.GetStateRequest{SessionId: m.sessionID}); err == nil {
		m.applyStateSync(state)
	}
}

func (m *model) applyStateSync(state *pb.GetStateResponse) {
	m.modelName = state.Model
	m.provider = state.Provider
	m.thinking = state.ThinkingLevel
	if state.ProviderInfo != nil {
		m.contextWindow = int(state.ProviderInfo.ContextWindow)
	}
}

func (m *model) updatePromptHistory(msgs []*pb.ConversationMessage) {
	m.promptHistory = make([]string, 0)
	seen := make(map[string]bool)
	for _, msg := range msgs {
		if msg.Role == "user" && msg.Content != "" && msg.Content != "Continue" {
			if !seen[msg.Content] {
				m.promptHistory = append(m.promptHistory, msg.Content)
				seen[msg.Content] = true
			}
		}
	}
	m.historyIndex = -1
}

func (m *model) ensureAssistantEntry() *historyEntry {
	if len(m.history) > 0 && m.history[len(m.history)-1].role == "assistant" {
		last := &m.history[len(m.history)-1]
		if len(last.items) == 0 || !m.newAssistantEntry {
			m.newAssistantEntry = false
			return last
		}
	}

	if m.newAssistantEntry || len(m.history) == 0 || m.history[len(m.history)-1].role != "assistant" {
		m.history = append(m.history, historyEntry{role: "assistant"})
		m.newAssistantEntry = false
	}
	return &m.history[len(m.history)-1]
}

func listenForEvent(eventCh <-chan *pb.AgentEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-eventCh
		if !ok {
			return nil
		}
		return agentEventMsg{ev}
	}
}

// extractFirstArgument pulls a summary of the tool arguments. For most tools, it's the first
// string value. For tools like write/edit, it includes multiple relevant fields.
func extractFirstArgument(toolName, argsJSON string) string {
	if argsJSON == "" {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return argsJSON
	}

	// Special handling for write/edit to show both path and content
	if toolName == "write" || toolName == "edit" || toolName == "read" {
		var path, content string
		if v, ok := m["path"]; ok {
			_ = json.Unmarshal(v, &path)
		}
		if v, ok := m["content"]; ok {
			_ = json.Unmarshal(v, &content)
		}
		if v, ok := m["replacement"]; ok && content == "" {
			_ = json.Unmarshal(v, &content)
		}

		if path != "" && content != "" {
			return path + "\n" + content
		}
		if path != "" {
			return path
		}
		if content != "" {
			return content
		}
	}

	// Prioritize common "identifier" fields
	priority := []string{"path", "filename", "id", "cmd", "command", "name", "url", "query"}
	for _, key := range priority {
		if v, ok := m[key]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				return s
			}
		}
	}

	// Fallback to first string field that isn't too long
	for _, v := range m {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			if len(s) < 100 {
				return s
			}
		}
	}

	// Last resort: just pick something
	for _, v := range m {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return s
		}
	}

	return argsJSON
}

// getAgentStats retrieves stats from the service for display in modals.
func (m *model) getAgentStats() agentStats {
	state, err := m.client.GetState(context.Background(), &pb.GetStateRequest{SessionId: m.sessionID})
	if err != nil {
		return agentStats{SessionID: m.sessionID}
	}
	msgs, _ := m.client.GetMessages(context.Background(), &pb.GetMessagesRequest{SessionId: m.sessionID})
	var user, asst, toolCalls, toolResults int
	if msgs != nil {
		for _, msg := range msgs.Messages {
			switch msg.Role {
			case "user":
				user++
			case "assistant":
				asst++
				toolCalls += len(msg.ToolCalls)
			case "tool":
				toolResults++
			}
		}
	}
	total := user + asst + toolResults
	return agentStats{
		SessionID:    m.sessionID,
		Name:         state.GetModel(),
		Model:        state.Model,
		Provider:     state.Provider,
		Thinking:     state.ThinkingLevel,
		SessionFile:  m.sessionMgr.SessionPath(m.sessionID),
		UserMessages: user,
		AssistantMsgs: asst,
		ToolCalls:    toolCalls,
		ToolResults:  toolResults,
		TotalMessages: total,
		ContextTokens: m.tokens,
		ContextWindow: m.contextWindow,
	}
}

// agentStats mirrors the data previously provided by agent.AgentStats.
type agentStats struct {
	SessionID      string
	ParentID       string
	Name           string
	Model          string
	Provider       string
	Thinking       string
	SessionFile    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	UserMessages   int
	AssistantMsgs  int
	ToolCalls      int
	ToolResults    int
	TotalMessages  int
	InputTokens    int
	OutputTokens   int
	CacheRead      int
	CacheWrite     int
	TotalTokens    int
	ContextTokens  int
	ContextWindow  int
	Cost           float64
	QueuedSteer    int
	QueuedFollowUp int
}

