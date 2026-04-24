package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/goppydae/gollm/internal/events"
	"github.com/goppydae/gollm/internal/llm"
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
	EventAgentStart     EventType = "agent_start"
	EventTurnStart      EventType = "turn_start"
	EventMessageStart   EventType = "message_start"
	EventTextDelta      EventType = "text_delta"
	EventThinkingDelta  EventType = "thinking_delta"
	EventToolCall       EventType = "tool_call"
	EventToolDelta      EventType = "tool_delta"
	EventToolOutput     EventType = "tool_output"
	EventMessageEnd     EventType = "message_end"
	EventAgentEnd       EventType = "agent_end"
	EventError          EventType = "error"
	EventAbort          EventType = "abort"
	EventQueueUpdate    EventType = "queue_update"
	EventCompactStart   EventType = "compact_start"
	EventCompactEnd     EventType = "compact_end"
	EventStateChange    EventType = "state_change"
	EventTokens         EventType = "tokens"
)

// Agent owns the transcript, emits events, and executes tools.
type Agent struct {
	state    *AgentState
	provider llm.Provider
	tools    *tools.ToolRegistry
	events   *events.EventBus
	stop         chan struct{}
	stopping     atomic.Bool
	done         chan struct{}
	mu           sync.RWMutex
	lifeState    *StateMachine
	extensions   []Extension
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
		ag.events.Publish(Event{
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

// SetModel sets the model name.
func (a *Agent) SetModel(model string) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Model = model
	a.state.Session.Model = model
}

// SetThinkingLevel sets the thinking level.
func (a *Agent) SetThinkingLevel(level ThinkingLevel) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Thinking = level
	a.state.Session.Thinking = level
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

// SetSessionName sets a human-readable name for the current session.
func (a *Agent) SetSessionName(name string) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Session.Name = name
}

// SetProvider sets the LLM provider.
func (a *Agent) SetProvider(provider llm.Provider) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Provider = provider.Info().Name
	a.state.Session.Provider = provider.Info().Name
	a.state.Model = provider.Info().Model
	a.state.Session.Model = provider.Info().Model
	a.provider = provider
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
	a.stopping.Store(false)

	// Add user message
	msg := Message{Role: "user", Content: text}
	if len(images) > 0 {
		msg.Images = images
	}

	a.mu.Lock()
	a.state.Messages = append(a.state.Messages, msg)
	a.state.Session.UpdatedAt = time.Now()
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

// Idle returns a channel that closes when the agent is idle.
func (a *Agent) Idle() <-chan struct{} {
	return a.done
}

// Messages returns the conversation messages.
func (a *Agent) Messages() []Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state.Messages
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
// Compact trims the transcript to stay within approximate token budgets.
// Keeps the first anchorN messages and the most recent tail.
func (a *Agent) Compact(keepRecentTokens int) {
	from := a.lifeState.Current()
	// Transition to compacting. If we fail (e.g. already in a sensitive state), abort compaction.
	if err := a.lifeState.Transition(StateCompacting); err != nil {
		return
	}
	defer func() {
		if a.lifeState.Current() == StateCompacting {
			_ = a.lifeState.Transition(from)
		}
	}()

	a.events.Publish(Event{Type: EventCompactStart})
	defer a.events.Publish(Event{Type: EventCompactEnd})

	a.mu.Lock()
	defer a.mu.Unlock()
	const anchorN = 2
	if len(a.state.Messages) <= anchorN+2 {
		return
	}

	// Rough heuristic: 4 chars ≈ 1 token.
	budget := keepRecentTokens
	tail := a.state.Messages[anchorN:]
	kept := 0
	tokens := 0
	for i := len(tail) - 1; i >= 0; i-- {
		tokens += EstimateMessageTokens(tail[i])
		if tokens > budget {
			break
		}
		kept = len(tail) - i
	}
	if kept == 0 {
		kept = 2
	}
	start := len(a.state.Messages) - kept
	if start <= anchorN {
		return
	}
	a.state.Messages = append(a.state.Messages[:anchorN:anchorN], a.state.Messages[start:]...)
}

func EstimateMessageTokens(m Message) int {
	// Rough heuristic: 4 chars ≈ 1 token.
	count := len(m.Content) / 4
	count += len(m.Thinking) / 4
	for _, tc := range m.ToolCalls {
		count += (len(tc.Name) + len(tc.Args)) / 4
	}
	if m.ToolCallID != "" {
		count += len(m.ToolCallID) / 4
	}
	return count
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

// LoadSession replaces the agent's state with the given session.
func (a *Agent) LoadSession(sess *types.Session) {
	if a.IsRunning() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Session = *sess
	a.state.Messages = make([]Message, len(sess.Messages))
	copy(a.state.Messages, sess.Messages)

	if sess.Model != "" {
		a.state.Model = sess.Model
	} else {
		a.state.Session.Model = a.state.Model
	}

	if sess.Provider != "" {
		a.state.Provider = sess.Provider
	} else {
		a.state.Session.Provider = a.state.Provider
	}

	if sess.Thinking != "" {
		a.state.Thinking = ThinkingLevel(sess.Thinking)
	} else {
		a.state.Session.Thinking = a.state.Thinking
	}

	a.state.SystemPrompt = sess.SystemPrompt
	a.state.MaxTokens = sess.MaxTokens
	a.state.Temperature = sess.Temperature
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
	a.stopping.Store(false)

	// 1. Create a tool call ID
	id := "user-call-" + uuid.New().String()[:8]

	// 2. Add assistant message with tool call
	a.mu.Lock()
	a.state.Messages = append(a.state.Messages, Message{
		Role: "assistant",
		ToolCalls: []types.ToolCall{{
			ID:   id,
			Name: name,
			Args: json.RawMessage(args),
		}},
	})
	a.mu.Unlock()

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
				close(a.done)
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

// GetSession returns a copy of the current session.
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
	total := EstimateMessageTokens(Message{Role: "system", Content: a.state.SystemPrompt})
	for _, m := range a.state.Messages {
		total += EstimateMessageTokens(m)
	}
	return total
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
		SessionID:     a.state.Session.ID,
		SessionFile:   "", // Will be set by caller
		Name:          a.state.Session.Name,
		CreatedAt:     a.state.Session.CreatedAt,
		UpdatedAt:     a.state.Session.UpdatedAt,
		Model:         a.state.Model,
		Provider:      a.state.Provider,
		Thinking:      string(a.state.Thinking),
		UserMessages:  userMsgs,
		AssistantMsgs: assistantMsgs,
		ToolCalls:     toolCalls,
		ToolResults:   toolResults,
		TotalMessages: userMsgs + assistantMsgs + toolResults,
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
	SessionID     string
	ParentID      string
	SessionFile   string
	Name          string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Model         string
	Provider      string
	Thinking      string
	UserMessages  int
	AssistantMsgs int
	ToolCalls     int
	ToolResults   int
	TotalMessages int
	InputTokens   int
	OutputTokens  int
	CacheRead     int
	CacheWrite    int
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
