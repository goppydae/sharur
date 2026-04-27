package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/goppydae/gollm/internal/events"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// Event represents an agent lifecycle event.
type Event struct {
	Type     EventType
	Content  string
	ToolCall *ToolCall
	Usage    *llm.Usage
	Error    error
	// ToolOutput stores the result content of a tool execution.
	// Emitted when type is EventToolOutput.
	ToolOutput *ToolOutput
	// StateChange holds details of a lifecycle state transition.
	// Emitted when type is EventStateChange.
	StateChange *StateTransition
	// Value stores a numeric value (e.g. token count).
	// Emitted when type is EventTokens.
	Value int64
}

// EventType identifies the kind of agent event.
type EventType string

const (
	EventAgentStart    EventType = "agent_start"
	EventTurnStart     EventType = "turn_start"
	EventMessageStart  EventType = "message_start"
	EventTextDelta     EventType = "text_delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventToolCall      EventType = "tool_call"
	EventToolDelta     EventType = "tool_delta"
	EventToolOutput    EventType = "tool_output"
	EventMessageEnd    EventType = "message_end"
	EventAgentEnd      EventType = "agent_end"
	EventError         EventType = "error"
	EventAbort         EventType = "abort"
	EventQueueUpdate   EventType = "queue_update"
	EventCompactStart  EventType = "compact_start"
	EventCompactEnd    EventType = "compact_end"
	EventStateChange   EventType = "state_change"
	EventTokens        EventType = "tokens"
	EventHeartbeat     EventType = "heartbeat"
)

// summarySentinel is prepended to every generated summary so detection is reliable
// regardless of the LLM's exact phrasing of section headers.
const summarySentinel = "<!-- gollm-summary -->\n"

const SUMMARIZATION_PROMPT = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Start your response with the exact string: <!-- gollm-summary -->

Then use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const UPDATE_SUMMARIZATION_PROMPT = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Start your response with the exact string: <!-- gollm-summary -->

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const TURN_PREFIX_SUMMARIZATION_PROMPT = `This is the PREFIX of a turn that was too large to keep. The SUFFIX (recent work) is retained.

Summarize the prefix to provide context for the retained suffix:

## Original Request
[What did the user ask for in this turn?]

## Early Progress
- [Key decisions and work done in the prefix]

## Context for Suffix
- [Information needed to understand the retained recent work]

Be concise. Focus on what's needed to understand the kept suffix.`

// Agent owns the transcript, emits events, and executes tools.
type Agent struct {
	state      *AgentState
	provider   llm.Provider
	tools      *tools.ToolRegistry
	events     *events.EventBus
	stop       chan struct{}
	stopping   atomic.Bool
	done       chan struct{}
	doneOnce   sync.Once
	mu         sync.RWMutex
	lifeState  *StateMachine
	extensions []Extension
	dryRun     bool
	compaction struct {
		Enabled          bool
		ReserveTokens    int
		KeepRecentTokens int
	}

	// New: Session management
	mgr  *session.Manager
	sess *session.Session
}

// GetInfo returns the current model's provider info.
func (a *Agent) GetInfo() llm.ProviderInfo {
	return a.provider.Info()
}

// New creates a new agent with the given provider and tools.
func New(provider llm.Provider, registry *tools.ToolRegistry) *Agent {
	info := provider.Info()
	ag := &Agent{
		state: &AgentState{
			Session: types.Session{
				ID:           generateSessionID(),
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				Model:        info.Model,
				Provider:     info.Name,
				Thinking:     ThinkingMedium,
				SystemPrompt: "You are a helpful coding assistant.",
			},
			Model:        info.Model,
			Provider:     info.Name,
			SystemPrompt: "You are a helpful coding assistant.",
			Thinking:     ThinkingMedium,
		},
		provider: provider,
		tools:    registry,
		events:   events.NewEventBus(),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	ag.lifeState = NewStateMachine(StateIdle, func(st StateTransition) {
		go ag.events.Publish(Event{
			Type:        EventStateChange,
			StateChange: &st,
		})
	})
	return ag
}

// State returns a copy of the current agent state.
func (a *Agent) State() *AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Deep copy
	stateCopy := *a.state
	stateCopy.Messages = make([]Message, len(a.state.Messages))
	copy(stateCopy.Messages, a.state.Messages)
	stateCopy.Tools = make([]ToolInfo, len(a.state.Tools))
	copy(stateCopy.Tools, a.state.Tools)
	stateCopy.Compaction.Enabled = a.compaction.Enabled
	stateCopy.Compaction.ReserveTokens = a.compaction.ReserveTokens
	stateCopy.Compaction.KeepRecentTokens = a.compaction.KeepRecentTokens
	if a.state.LatestCompaction != nil {
		lc := *a.state.LatestCompaction
		stateCopy.LatestCompaction = &lc
	}
	return &stateCopy
}

// SetSystemPrompt updates the system prompt.
func (a *Agent) SetSystemPrompt(prompt string) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.SystemPrompt = prompt
}

// SetModel sets the model name and records it in the session if manager is present.
func (a *Agent) SetModel(model string) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Model = model
	if a.mgr != nil && a.sess != nil && a.state.Provider != "" {
		_ = a.mgr.AppendModelChange(a.sess, a.state.Provider, model)
	}
}

// SetThinkingLevel sets the thinking level and records it in the session if manager is present.
func (a *Agent) SetThinkingLevel(level ThinkingLevel) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Thinking = level
	if a.mgr != nil && a.sess != nil {
		_ = a.mgr.AppendThinkingLevelChange(a.sess, string(level))
	}
}

// SetMaxTokens sets the maximum tokens for LLM responses.
func (a *Agent) SetMaxTokens(n int) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.MaxTokens = n
}

// SetCompactionConfig updates the compaction settings.
func (a *Agent) SetCompactionConfig(enabled bool, reserve, keepRecent int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.compaction.Enabled = enabled
	a.compaction.ReserveTokens = reserve
	a.compaction.KeepRecentTokens = keepRecent
}

// closeDone closes a.done exactly once per Prompt/InvokeTool lifecycle.
func (a *Agent) closeDone() {
	a.doneOnce.Do(func() { close(a.done) })
}

// SetDryRun sets the agent's dry-run mode.
func (a *Agent) SetDryRun(dry bool) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dryRun = dry
	a.state.DryRun = dry
}

// SetSessionName sets a human-readable name for the current session.
func (a *Agent) SetSessionName(name string) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Session.Name = name
}

// SetProvider sets the LLM provider and records it in the session if manager is present.
func (a *Agent) SetProvider(provider llm.Provider) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	info := provider.Info()
	a.state.Provider = info.Name
	a.state.Model = info.Model
	a.provider = provider

	if a.mgr != nil && a.sess != nil {
		_ = a.mgr.AppendModelChange(a.sess, info.Name, info.Model)
	}
}

// SetSession attaches a session manager and session to the agent.
func (a *Agent) SetSession(mgr *session.Manager, sess *session.Session) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mgr = mgr
	a.sess = sess
	if sess != nil {
		a.state.Messages = sess.Messages
		if sess.Model != "" {
			a.state.Model = sess.Model
		}
		if sess.Provider != "" {
			a.state.Provider = sess.Provider
		}
		if sess.Thinking != "" {
			a.state.Thinking = ThinkingLevel(sess.Thinking)
		}
		a.state.LatestCompaction = sess.LatestCompaction
		a.state.Session.ID = sess.ID
		a.state.Session.Name = sess.Name
	}
}

// SetExtensions sets the active extensions for the agent.
func (a *Agent) SetExtensions(exts []Extension) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.extensions = exts

	// Also register their tools
	for _, ext := range exts {
		for _, t := range ext.Tools() {
			a.tools.Register(t)
		}
	}
}

// Subscribe registers an event listener and returns an unsubscribe function.
func (a *Agent) Subscribe(fn func(Event)) func() {
	return a.events.Subscribe(func(e any) {
		if ev, ok := e.(Event); ok {
			fn(ev)
		}
	})
}

// Prompt sends a user message and runs the agent loop until idle.
func (a *Agent) Prompt(ctx context.Context, text string, images ...Image) error {
	if a.IsRunning() {
		return fmt.Errorf("agent is already running")
	}

	// Reset channels (prior goroutine finished — IsRunning was false above).
	a.stop = make(chan struct{})
	a.done = make(chan struct{})
	a.doneOnce = sync.Once{}
	a.stopping.Store(false)

	// Add user message
	msg := Message{Role: "user", Content: text, Timestamp: time.Now()}
	if len(images) > 0 {
		msg.Images = images
	}

	a.mu.Lock()
	if a.state.Session.Name == "" && text != "" && text != "Continue" {
		a.state.Session.Name = text
		if a.mgr != nil && a.sess != nil {
			_ = a.mgr.AppendSessionInfo(a.sess, text)
		}
	}

	if a.mgr != nil && a.sess != nil {
		_, _ = a.mgr.AppendMessage(a.sess, msg)
		a.state.Messages = a.sess.Messages // Sync back
	} else {
		a.state.Messages = append(a.state.Messages, msg)
	}
	a.mu.Unlock()

	// Emit event
	a.events.Publish(Event{Type: EventAgentStart})

	go a.runTurn(ctx)

	return nil
}

// Continue asks the agent to continue generating.
func (a *Agent) Continue(ctx context.Context) error {
	if !a.IsRunning() {
		return fmt.Errorf("agent is not running")
	}

	// Add a continuation marker
	a.mu.Lock()
	a.state.Messages = append(a.state.Messages, Message{Role: "user", Content: "Continue"})
	a.mu.Unlock()

	a.events.Publish(Event{Type: EventTurnStart})

	go a.runTurn(ctx)
	return nil
}

// Steer queues a steering message to be injected as soon as the current tool execution finishes.
func (a *Agent) Steer(text string, images ...Image) {
	a.mu.Lock()
	msg := Message{Role: "user", Content: text}
	if len(images) > 0 {
		msg.Images = images
	}
	a.state.SteerQueue = append(a.state.SteerQueue, msg)
	a.mu.Unlock()
	a.events.Publish(Event{Type: EventQueueUpdate})
}

// FollowUp queues a follow-up message to be processed after the agent finishes.
func (a *Agent) FollowUp(text string, images ...Image) {
	a.mu.Lock()
	msg := Message{Role: "user", Content: text}
	if len(images) > 0 {
		msg.Images = images
	}
	a.state.FollowUpQueue = append(a.state.FollowUpQueue, msg)
	a.mu.Unlock()
	a.events.Publish(Event{Type: EventQueueUpdate})
}

// Abort signals the agent to stop the current turn.
func (a *Agent) Abort() {
	if a.stopping.Swap(true) {
		return // Already aborting
	}
	close(a.stop)
	a.events.Publish(Event{Type: EventAbort})
}

// IsRunning reports whether the agent is currently processing.
func (a *Agent) IsRunning() bool {
	return a.lifeState.Current() != StateIdle
}

// LifecycleState returns the current lifecycle state as a string.
func (a *Agent) LifecycleState() string {
	return string(a.lifeState.Current())
}

// Idle returns a channel that closes when the agent is idle.
func (a *Agent) Idle() <-chan struct{} {
	return a.done
}

// Messages returns a copy of the conversation messages.
func (a *Agent) Messages() []Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	res := make([]Message, len(a.state.Messages))
	copy(res, a.state.Messages)
	return res
}

// ToolRegistry returns the tool registry.
func (a *Agent) ToolRegistry() *tools.ToolRegistry {
	return a.tools
}

// EventBus returns the event bus.
func (a *Agent) EventBus() *events.EventBus {
	return a.events
}

// Compact trims the transcript to stay within approximate token budgets.
// It implements a pi-mono style summarization and file tracking strategy.
func (a *Agent) Compact(ctx context.Context, keepRecentTokens int) {
	from := a.lifeState.Current()
	if from == StateCompacting || from == StateAborting {
		return
	}

	// 1. Snapshot and Guard
	a.mu.Lock()

	if err := a.lifeState.Transition(StateCompacting); err != nil {
		a.mu.Unlock()
		return
	}

	var freed int
	// Signal start with a descriptive value for the TUI
	a.events.Publish(Event{Type: EventCompactStart, Content: "Compacting session context..."})

	defer func() {
		// Fix #2: restore the lifecycle state we entered from so that a manual
		// /compact (called while idle) doesn't leave the state machine stuck in
		// StateCompacting, which would freeze the TUI and block new Prompt calls.
		_ = a.lifeState.Transition(from)

		// Fix #4: release the lock before publishing so that synchronous
		// Subscribe() callbacks can safely read agent state without deadlocking.
		content := ""
		if freed > 0 {
			content = fmt.Sprintf("Context compacted. Freed %d tokens.", freed)
		}
		a.mu.Unlock()
		a.events.Publish(Event{Type: EventCompactEnd, Value: int64(freed), Content: content})
	}()

	tokensBefore := a.estimateContextTokensNoLock()
	messages := a.state.Messages
	if len(messages) <= 1 {
		return
	}

	// 2. Identify previous summary and boundary
	var previousSummary string
	boundaryStart := 0
	if a.state.LatestCompaction != nil {
		previousSummary = a.state.LatestCompaction.Summary
		for i, m := range messages {
			if m.ID == a.state.LatestCompaction.FirstKeptEntryID {
				boundaryStart = i
				break
			}
		}
	}

	// 3. Find Cut Point
	// Fix #5: deduct system-prompt tokens and a rough summary-overhead allowance
	// from the kept-tail budget. Without this, post-compaction total tokens
	// (system + summary + tail) can still exceed the threshold, causing an
	// immediate re-compaction on the very next turn.
	const summaryOverheadTokens = 2048
	sysTokens := a.estimateMessageTokens(Message{Content: a.state.SystemPrompt})
	effectiveBudget := keepRecentTokens - sysTokens - summaryOverheadTokens
	if effectiveBudget < 512 {
		effectiveBudget = 512
	}
	cutResult := a.findCutPoint(messages, boundaryStart, len(messages), effectiveBudget)
	if cutResult.FirstKeptIndex <= boundaryStart {
		// Nothing to compact or we already reached the limit
		return
	}

	// 4. Determine summarizing ranges
	historyEnd := cutResult.FirstKeptIndex
	if cutResult.IsSplitTurn {
		historyEnd = cutResult.TurnStartIndex
	}

	messagesToSummarize := messages[boundaryStart:historyEnd]
	var turnPrefixMessages []Message
	if cutResult.IsSplitTurn {
		turnPrefixMessages = messages[cutResult.TurnStartIndex:cutResult.FirstKeptIndex]
	}

	// 5. Extract File Context
	// Release the lock during context extraction and LLM calls for responsiveness
	a.mu.Unlock()

	readFiles, modFiles := a.extractFileContext(messagesToSummarize)
	if cutResult.IsSplitTurn {
		r, m := a.extractFileContext(turnPrefixMessages)
		readFiles = append(readFiles, r...)
		modFiles = append(modFiles, m...)
	}
	// Fix #10: carry forward file activity recorded in the previous summary so
	// files touched before the last compaction boundary are not silently lost.
	if previousSummary != "" {
		pr, pm := parseFileActivityFromSummary(previousSummary)
		readFiles = append(readFiles, pr...)
		modFiles = append(modFiles, pm...)
	}

	// 6. Generate Summaries via LLM
	summaryChan := make(chan string, 1)
	errChan := make(chan error, 1)
	go func() {
		s, err := a.generateSummary(ctx, messagesToSummarize, previousSummary)
		if err != nil {
			errChan <- err
			return
		}
		summaryChan <- s
	}()

	var turnPrefixSummary string
	if cutResult.IsSplitTurn {
		s, err := a.generateTurnPrefixSummary(ctx, turnPrefixMessages)
		if err != nil {
			turnPrefixSummary = "## Turn Prefix Summary\n(Summarization failed)"
		} else {
			turnPrefixSummary = s
		}
	}

	var summaryText string
	select {
	case s := <-summaryChan:
		summaryText = s
	case err := <-errChan:
		summaryText = "## Session Progress Summary\n(Summarization failed: " + err.Error() + ")"
	case <-ctx.Done():
		// Re-acquire the lock so the deferred unlock is balanced, then bail.
		a.mu.Lock()
		return
	case <-a.stop:
		// Re-acquire the lock so the deferred unlock is balanced, then bail.
		a.mu.Lock()
		return
	}

	// Append file activity
	summaryText += a.formatFileActivity(readFiles, modFiles)

	if cutResult.IsSplitTurn && turnPrefixSummary != "" {
		summaryText += "\n\n### Split Turn Context\n" + turnPrefixSummary
	}

	a.mu.Lock()

	// RE-VALIDATE: Did the messages change while we were unlocked?
	// If messages were removed or the cut index is now out of bounds, abort.
	if len(a.state.Messages) < len(messages) || cutResult.FirstKeptIndex > len(a.state.Messages) {
		return
	}

	// 7. Record Compaction State
	firstKeptID := ""
	if cutResult.FirstKeptIndex < len(a.state.Messages) {
		firstKeptID = a.state.Messages[cutResult.FirstKeptIndex].ID
	}
	
	a.state.LatestCompaction = &types.CompactionState{
		Summary:          summaryText,
		FirstKeptEntryID: firstKeptID,
	}
	
	tokensAfter := a.estimateContextTokensNoLock()
	freed = tokensBefore - tokensAfter
	if freed < 0 {
		freed = 0
	}

	// Add a concise compaction notice for the history/TUI instead of the full summary
	compactionMsg := Message{
		ID:        uuid.New().String(),
		Role:      "compaction",
		Content:   fmt.Sprintf("Context compacted. Freed %d tokens.", freed),
		Timestamp: time.Now(),
	}
	
	a.state.Messages = append(a.state.Messages, compactionMsg)

	// Record in session tree
	if a.mgr != nil && a.sess != nil {
		_ = a.mgr.AppendCompaction(a.sess, summaryText, firstKeptID, tokensBefore, tokensAfter)
	}

	a.events.Publish(Event{Type: EventTokens, Value: int64(tokensAfter)})
}

func (a *Agent) appendMessage(msg Message) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.mgr != nil && a.sess != nil {
		_, _ = a.mgr.AppendMessage(a.sess, msg)
		a.state.Messages = a.sess.Messages
	} else {
		a.state.Messages = append(a.state.Messages, msg)
	}
}


type cutResult struct {
	FirstKeptIndex int
	TurnStartIndex int
	IsSplitTurn    bool
}

// findCutPoint finds the index to start keeping messages from.
func (a *Agent) findCutPoint(messages []Message, start, end, budget int) cutResult {
	accumulated := 0
	cutIndex := start

	// Walk backwards from newest
	for i := end - 1; i >= start; i-- {
		tokens := a.estimateMessageTokens(messages[i])
		if accumulated+tokens > budget {
			// Find closest valid cut point at or after this entry
			cutIndex = i
			// Adjust to avoid cutting mid-tool-call
			for j := i; j < end; j++ {
				if a.isValidCutPoint(messages, j) {
					cutIndex = j
					break
				}
			}
			break
		}
		accumulated += tokens
	}

	if cutIndex <= start {
		return cutResult{FirstKeptIndex: start}
	}

	// Determine if we are splitting a turn
	isUser := messages[cutIndex].Role == "user"
	turnStart := -1
	if !isUser {
		turnStart = a.findTurnStartIndex(messages, cutIndex, start)
	}

	return cutResult{
		FirstKeptIndex: cutIndex,
		TurnStartIndex: turnStart,
		IsSplitTurn:    !isUser && turnStart != -1,
	}
}

func (a *Agent) isValidCutPoint(messages []Message, idx int) bool {
	role := messages[idx].Role
	// Don't cut at tool results (role="tool") as they must follow assistant calls
	return role == "user" || role == "assistant" || role == "success"
}

func (a *Agent) findTurnStartIndex(messages []Message, idx, start int) int {
	for i := idx; i >= start; i-- {
		if messages[i].Role == "user" {
			return i
		}
	}
	return -1
}

// generateSummary uses the LLM to create a structured summary of pruned messages.
func (a *Agent) generateSummary(ctx context.Context, messages []Message, previousSummary string) (string, error) {
	prompt := SUMMARIZATION_PROMPT
	if previousSummary != "" {
		prompt = UPDATE_SUMMARIZATION_PROMPT
	}

	conversationText := a.serializeConversation(messages)
	
	var promptText string
	if previousSummary != "" {
		promptText = fmt.Sprintf("<conversation>\n%s\n</conversation>\n\n<previous-summary>\n%s\n</previous-summary>\n\n%s", conversationText, previousSummary, prompt)
	} else {
		promptText = fmt.Sprintf("<conversation>\n%s\n</conversation>\n\n%s", conversationText, prompt)
	}

	req := &llm.CompletionRequest{
		Model:  a.state.Model,
		System: "You are a context compaction engine.",
		Messages: []types.Message{
			{Role: "user", Content: promptText},
		},
	}

	stream, err := a.provider.Stream(ctx, req)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for ev := range stream {
		switch ev.Type {
		case llm.EventTextDelta:
			sb.WriteString(ev.Content)
		case llm.EventError:
			return "", ev.Error
		}
	}

	return sb.String(), nil
}

func (a *Agent) generateTurnPrefixSummary(ctx context.Context, messages []Message) (string, error) {
	conversationText := a.serializeConversation(messages)
	promptText := fmt.Sprintf("<conversation>\n%s\n</conversation>\n\n%s", conversationText, TURN_PREFIX_SUMMARIZATION_PROMPT)

	req := &llm.CompletionRequest{
		Model:  a.state.Model,
		System: "You are a context compaction engine.",
		Messages: []types.Message{
			{Role: "user", Content: promptText},
		},
	}

	stream, err := a.provider.Stream(ctx, req)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for ev := range stream {
		switch ev.Type {
		case llm.EventTextDelta:
			sb.WriteString(ev.Content)
		case llm.EventError:
			return "", ev.Error
		}
	}

	return sb.String(), nil
}

func (a *Agent) serializeConversation(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		// Fix #8: include tool-call ID on tool-result messages so the summarising
		// LLM can correlate each result with the call that produced it.
		if m.ToolCallID != "" {
			fmt.Fprintf(&sb, "[%s|id=%s]: %s\n", m.Role, m.ToolCallID, m.Content)
		} else {
			fmt.Fprintf(&sb, "[%s]: %s\n", m.Role, m.Content)
		}
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&sb, "(tool_call[id=%s]: %s %s)\n", tc.ID, tc.Name, string(tc.Args))
		}
	}
	return sb.String()
}

func (a *Agent) formatFileActivity(read, mod []string) string {
	if len(read) == 0 && len(mod) == 0 {
		return ""
	}
	
	var sb strings.Builder
	sb.WriteString("\n\n### File Activity\n")
	if len(read) > 0 {
		sb.WriteString("- Read: " + strings.Join(read, ", ") + "\n")
	}
	if len(mod) > 0 {
		sb.WriteString("- Modified: " + strings.Join(mod, ", ") + "\n")
	}
	return sb.String()
}


// parseFileActivityFromSummary extracts the "### File Activity" section from a
// previously generated summary so file tracking is preserved across compaction
// cycles (fix #10).
func parseFileActivityFromSummary(summary string) (read []string, modified []string) {
	for _, line := range strings.Split(summary, "\n") {
		if after, ok := strings.CutPrefix(line, "- Read: "); ok {
			for _, f := range strings.Split(after, ", ") {
				if f = strings.TrimSpace(f); f != "" {
					read = append(read, f)
				}
			}
		} else if after, ok := strings.CutPrefix(line, "- Modified: "); ok {
			for _, f := range strings.Split(after, ", ") {
				if f = strings.TrimSpace(f); f != "" {
					modified = append(modified, f)
				}
			}
		}
	}
	return
}

// extractFileContext scans messages for file-related tool calls.
func (a *Agent) extractFileContext(messages []Message) (read []string, modified []string) {
	readMap := make(map[string]bool)
	modMap := make(map[string]bool)

	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			var args struct {
				Path       string `json:"path"`
				TargetFile string `json:"TargetFile"` // Common in some gollm tools
				Target     string `json:"target"`     // Common in others
			}
			_ = json.Unmarshal(tc.Args, &args)

			path := args.Path
			if path == "" { path = args.TargetFile }
			if path == "" { path = args.Target }
			if path == "" { continue }

			switch tc.Name {
			case "read", "ls", "grep", "find":
				readMap[path] = true
			case "write", "edit", "replace_file_content", "multi_replace_file_content":
				modMap[path] = true
			}
		}
	}

	for f := range readMap { read = append(read, f) }
	for f := range modMap { modified = append(modified, f) }
	sort.Strings(read)
	sort.Strings(modified)
	return
}

// estimateContextTokensNoLock estimates tokens without taking any locks.
func (a *Agent) estimateContextTokensNoLock() int {
	var total int
	// System prompt
	total += a.estimateMessageTokens(Message{Role: "system", Content: a.state.SystemPrompt})

	// Messages (use LLM-facing context messages)
	for _, m := range a.buildLlmMessagesNoLock() {
		total += a.estimateMessageTokens(m)
	}
	return total
}

// buildLlmMessages returns the slice of messages to be sent to the LLM.
func (a *Agent) buildLlmMessages() []Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.buildLlmMessagesNoLock()
}

func (a *Agent) buildLlmMessagesNoLock() []Message {
	if a.state.LatestCompaction == nil {
		msgs := make([]Message, len(a.state.Messages))
		copy(msgs, a.state.Messages)
		return msgs
	}

	lc := a.state.LatestCompaction
	res := make([]Message, 0, len(a.state.Messages)+1)
	
	// Add summary message
	res = append(res, Message{
		Role:    "success",
		Content: lc.Summary,
	})

	// Find the boundary and add the tail
	found := false
	for _, m := range a.state.Messages {
		if !found && m.ID == lc.FirstKeptEntryID {
			found = true
		}
		if found {
			res = append(res, m)
		}
	}
	
	// If boundary ID was not found (shouldn't happen with proper session management),
	// fallback to full history to avoid data loss.
	if !found && lc.FirstKeptEntryID != "" {
		return a.state.Messages
	}

	return res
}

func (a *Agent) estimateMessageTokens(m Message) int {
	if m.Usage != nil {
		return m.Usage.TotalTokens
	}
	return EstimateMessageTokens(m)
}

func EstimateMessageTokens(m Message) int {
	// Rough heuristic: 4 chars ≈ 1 token for ASCII; code and non-ASCII tokenise
	// more densely. Apply a 20% overhead to err on the side of early compaction
	// rather than hitting the context-window hard limit.
	count := len(m.Content) / 4
	count += len(m.Thinking) / 4
	for _, tc := range m.ToolCalls {
		count += (len(tc.Name) + len(tc.Args)) / 4
	}
	if m.ToolCallID != "" {
		count += len(m.ToolCallID) / 4
	}
	// Fix #7: approximate image tokens. Base64-encoded data is ~4/3 the raw byte
	// size; vision models charge roughly 1 token per 750 bytes of image data.
	// Using a fixed 1024-token floor per image avoids underestimating tiny images.
	for _, img := range m.Images {
		imgTokens := len(img.Data) / 750
		if imgTokens < 1024 {
			imgTokens = 1024
		}
		count += imgTokens
	}
	// 20% safety margin to compensate for the char/token heuristic underestimating
	// code blocks, non-ASCII text, and per-message formatting overhead.
	return count * 6 / 5
}

// Reset clears the conversation history and queues.
func (a *Agent) Reset() {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Messages = nil
	a.state.SteerQueue = nil
	a.state.FollowUpQueue = nil
	a.events.Publish(Event{Type: EventQueueUpdate})
}

// ResetSession clears messages, queues and creates a fresh session ID.
func (a *Agent) ResetSession(id string) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Messages = nil
	a.state.SteerQueue = nil
	a.state.FollowUpQueue = nil
	a.state.Session.ID = id
	a.state.Session.CreatedAt = time.Now()
	a.state.Session.UpdatedAt = time.Now()
	a.state.Session.Name = ""
	a.events.Publish(Event{Type: EventQueueUpdate})
}


// InvokeTool manually triggers a tool call as if it came from the assistant.
// It executes the tool, records the result, and then starts the agent loop
// to allow the LLM to react to the invocation.
func (a *Agent) InvokeTool(ctx context.Context, name string, args string) error {
	if a.IsRunning() {
		return fmt.Errorf("agent is already running")
	}

	// Reset channels
	a.stop = make(chan struct{})
	a.done = make(chan struct{})
	a.doneOnce = sync.Once{}
	a.stopping.Store(false)

	// 1. Create a tool call ID
	id := "user-call-" + uuid.New().String()[:8]

	// 2. Add assistant message with tool call
	a.appendMessage(Message{
		Role: "assistant",
		ToolCalls: []types.ToolCall{{
			ID:   id,
			Name: name,
			Args: json.RawMessage(args),
		}},
		Timestamp: time.Now(),
	})

	// 3. Emit events
	a.events.Publish(Event{Type: EventAgentStart})
	a.events.Publish(Event{Type: EventMessageStart})
	a.events.Publish(Event{
		Type: EventToolCall,
		ToolCall: &types.ToolCall{
			ID:   id,
			Name: name,
			Args: json.RawMessage(args),
		},
	})
	a.events.Publish(Event{Type: EventMessageEnd})

	// 4. Start processing in a background goroutine
	go func() {
		hasStartedTurn := false
		defer func() {
			if !hasStartedTurn {
				_ = a.lifeState.Transition(StateIdle)
				a.events.Publish(Event{Type: EventAgentEnd})
				a.closeDone()
			}
		}()

		_ = a.lifeState.Transition(StateExecuting)

		// Execute the tool
		tc := &llm.ToolCall{
			ID:   id,
			Name: name,
			Args: json.RawMessage(args),
		}
		if !a.runToolCalls(ctx, []*llm.ToolCall{tc}) {
			return
		}

		// Continue with the agent turn (reacting to the tool result)
		hasStartedTurn = true
		a.runTurn(ctx)
	}()

	return nil
}

// Session returns the current session object.
func (a *Agent) Session() *session.Session {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sess
}

// GetSession returns a copy of the current session types.
func (a *Agent) GetSession() *types.Session {
	a.mu.RLock()
	defer a.mu.RUnlock()
	sessCopy := a.state.Session
	sessCopy.Messages = make([]Message, len(a.state.Messages))
	copy(sessCopy.Messages, a.state.Messages)
	return &sessCopy
}

// EstimateContextTokens returns the estimated total tokens in the current context.
func (a *Agent) EstimateContextTokens() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.estimateContextTokensLocked()
}

func (a *Agent) estimateContextTokensLocked() int {
	return a.estimateContextTokensNoLock()
}

// GetStats returns token usage statistics from the agent's events.
func (a *Agent) GetStats() AgentStats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var userMsgs, assistantMsgs, toolCalls, toolResults int
	for _, m := range a.state.Messages {
		switch m.Role {
		case "user":
			userMsgs++
		case "assistant":
			assistantMsgs++
			toolCalls += len(m.ToolCalls)
		case "tool":
			toolResults++
		}
	}

	stats := AgentStats{
		SessionID:      a.state.Session.ID,
		SessionFile:    "", // Will be set by caller
		Name:           a.state.Session.Name,
		CreatedAt:      a.state.Session.CreatedAt,
		UpdatedAt:      a.state.Session.UpdatedAt,
		Model:          a.state.Model,
		Provider:       a.state.Provider,
		Thinking:       string(a.state.Thinking),
		UserMessages:   userMsgs,
		AssistantMsgs:  assistantMsgs,
		ToolCalls:      toolCalls,
		ToolResults:    toolResults,
		TotalMessages:  userMsgs + assistantMsgs + toolResults,
		QueuedSteer:    len(a.state.SteerQueue),
		QueuedFollowUp: len(a.state.FollowUpQueue),
		ContextTokens:  a.EstimateContextTokens(),
		ContextWindow:  a.provider.Info().ContextWindow,
	}
	if a.state.Session.ParentID != nil {
		stats.ParentID = *a.state.Session.ParentID
	}
	return stats
}

// AgentStats holds session statistics.
type AgentStats struct {
	SessionID      string
	ParentID       string
	SessionFile    string
	Name           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Model          string
	Provider       string
	Thinking       string
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

// generateSessionID creates a UUID v4 session ID.
func generateSessionID() string {
	return uuid.New().String()
}
