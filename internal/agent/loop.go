package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// runTurn is the core agentic loop: prompt → LLM → tools → LLM → ... until no more tool calls.
func (a *Agent) runTurn(ctx context.Context) {
	defer func() {
		_ = a.lifeState.Transition(StateIdle)
		a.events.Publish(Event{Type: EventAgentEnd})
		a.closeDone()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stop:
			return
		default:
		}

		// 1. Drain queues before next LLM call (or turn completion)
		if a.drainQueues() {
			continue
		}

		// 2. Auto-compaction check
		a.mu.RLock()
		compEnabled := a.compaction.Enabled
		compReserve := a.compaction.ReserveTokens
		compKeep := a.compaction.KeepRecentTokens
		a.mu.RUnlock()

		if compEnabled {
			tokens := a.EstimateContextTokens()
			info := a.provider.Info()
			if tokens > info.ContextWindow-compReserve {
				a.Compact(ctx, compKeep)
				if ctx.Err() != nil {
					return
				}
			}
		}

		_ = a.lifeState.Transition(StateThinking)

		// Run BeforePrompt extensions
		a.mu.Lock()
		for _, ext := range a.extensions {
			func() {
				defer func() {
					if r := recover(); r != nil {
						a.events.Publish(Event{Type: EventError, Error: fmt.Errorf("extension BeforePrompt panic: %v", r)})
					}
				}()
				if next := ext.BeforePrompt(ctx, a.state); next != nil {
					a.state = next
				}
			}()
		}
		// Apply ModifySystemPrompt so extensions (skills, plugins) can inject content.
		for _, ext := range a.extensions {
			func() {
				defer func() {
					if r := recover(); r != nil {
						a.events.Publish(Event{Type: EventError, Error: fmt.Errorf("extension ModifySystemPrompt panic: %v", r)})
					}
				}()
				a.state.SystemPrompt = ext.ModifySystemPrompt(a.state.SystemPrompt)
			}()
		}
		a.mu.Unlock()

		req := a.buildRequest()
		a.events.Publish(Event{Type: EventTurnStart})
		a.events.Publish(Event{Type: EventTokens, Value: int64(a.EstimateContextTokens())})

		stream, err := a.provider.Stream(ctx, req)
		if err != nil {
			_ = a.lifeState.Transition(StateError)
			a.events.Publish(Event{Type: EventError, Error: err})
			return
		}

		a.events.Publish(Event{Type: EventMessageStart})

		content, thinking, llmCalls, usage, ok := a.consumeStream(ctx, stream)
		if !ok {
			return
		}

		// Append assistant message with any tool calls
		assistantMsg := types.Message{Role: "assistant", Content: content, Thinking: thinking, Usage: usage}
		for _, tc := range llmCalls {
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, types.ToolCall{
				ID:       tc.ID,
				Name:     tc.Name,
				Args:     tc.Args,
				Position: tc.Position,
			})
		}
		assistantMsg.Timestamp = time.Now()
		a.appendMessage(assistantMsg)

		if len(llmCalls) == 0 {
			// Check queues again. If anything was added during stream, start new turn.
			if a.drainQueues() {
				continue
			}
			return
		}

		_ = a.lifeState.Transition(StateExecuting)
		if !a.runToolCalls(ctx, llmCalls) {
			return
		}
		// Loop: send tool results back to the LLM for the next turn.
	}
}

// drainQueues processes steer and follow-up messages from their respective queues.
// Returns true if any messages were processed, suggesting a new turn should start.
func (a *Agent) drainQueues() bool {
	a.mu.Lock()
	steer := a.state.SteerQueue
	a.state.SteerQueue = nil
	followUp := a.state.FollowUpQueue
	a.state.FollowUpQueue = nil
	a.mu.Unlock()

	if len(steer) == 0 && len(followUp) == 0 {
		return false
	}

	a.events.Publish(Event{Type: EventQueueUpdate})

	// Steer messages take priority
	msgs := append(steer, followUp...)
	for _, msg := range msgs {
		a.events.Publish(Event{Type: EventMessageStart})
		a.appendMessage(msg)
		a.events.Publish(Event{Type: EventMessageEnd})
	}

	return true
}

// buildRequest snapshots current state into a CompletionRequest.
func (a *Agent) buildRequest() *llm.CompletionRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()

	msgs := a.buildLlmMessagesNoLock()
	req := &llm.CompletionRequest{
		Model:       a.state.Model,
		Messages:    msgs,
		System:      a.state.SystemPrompt,
		Thinking:    a.state.Thinking,
		MaxTokens:   a.state.MaxTokens,
		Temperature: a.state.Temperature,
	}
	if a.tools != nil {
		for _, t := range a.tools.All() {
			req.Tools = append(req.Tools, types.ToolInfo{
				Name:        t.Name(),
				Description: t.Description(),
				Schema:      t.Schema(),
			})
		}
	}
	return req
}

// consumeStream drains the LLM event stream, publishing agent events and collecting tool calls.
// Returns (textContent, thinkingContent, toolCalls, ok). ok=false means the turn was aborted or errored.
func (a *Agent) consumeStream(ctx context.Context, stream <-chan *llm.Event) (string, string, []*llm.ToolCall, *types.Usage, bool) {
	var sb strings.Builder
	var thinkingSb strings.Builder
	var toolCalls []*llm.ToolCall
	var usage *types.Usage
	sentEnd := false

	for {
		select {
		case <-a.stop:
			_ = a.lifeState.Transition(StateAborting)
			a.events.Publish(Event{Type: EventAbort})
			return "", "", nil, nil, false
		case <-ctx.Done():
			_ = a.lifeState.Transition(StateAborting)
			a.events.Publish(Event{Type: EventAbort})
			return "", "", nil, nil, false
		case ev, ok := <-stream:
			if !ok {
				if !sentEnd {
					a.events.Publish(Event{Type: EventMessageEnd})
				}
				return sb.String(), thinkingSb.String(), toolCalls, usage, true
			}
			switch ev.Type {
			case llm.EventTextDelta:
				sb.WriteString(ev.Content)
				a.events.Publish(Event{Type: EventTextDelta, Content: ev.Content})
			case llm.EventThinkingDelta:
				thinkingSb.WriteString(ev.Content)
				a.events.Publish(Event{Type: EventThinkingDelta, Content: ev.Content})
			case llm.EventToolCall:
				if ev.ToolCall != nil {
					toolCalls = append(toolCalls, ev.ToolCall)
					a.events.Publish(Event{
						Type: EventToolCall,
						ToolCall: &types.ToolCall{
							ID:       ev.ToolCall.ID,
							Name:     ev.ToolCall.Name,
							Args:     ev.ToolCall.Args,
							Position: ev.ToolCall.Position,
						},
					})
				}
			case llm.EventMessageEnd:
				sentEnd = true
				if ev.Usage != nil {
					usage = ev.Usage
					a.events.Publish(Event{Type: EventMessageEnd, Usage: ev.Usage})
					a.events.Publish(Event{Type: EventTokens, Value: int64(ev.Usage.TotalTokens)})
				} else {
					a.events.Publish(Event{Type: EventMessageEnd})
				}
			case llm.EventError:
				_ = a.lifeState.Transition(StateError)
				a.events.Publish(Event{Type: EventError, Error: ev.Error})
				return "", "", nil, nil, false
			}
		}
	}
}

// runToolCalls executes each tool call and appends results to the transcript.
// Returns false if aborted.
func (a *Agent) runToolCalls(ctx context.Context, toolCalls []*llm.ToolCall) bool {
	for _, tc := range toolCalls {
		select {
		case <-a.stop:
			_ = a.lifeState.Transition(StateAborting)
			a.events.Publish(Event{Type: EventAbort})
			return false
		case <-ctx.Done():
			_ = a.lifeState.Transition(StateAborting)
			a.events.Publish(Event{Type: EventAbort})
			return false
		default:
		}

		// Give extensions a chance to intercept before execution.
		var result *tools.ToolResult
		a.mu.Lock()
		for _, ext := range a.extensions {
			func() {
				defer func() {
					if r := recover(); r != nil {
						a.events.Publish(Event{Type: EventError, Error: fmt.Errorf("extension BeforeToolCall panic: %v", r)})
					}
				}()
				if r, intercepted := ext.BeforeToolCall(ctx, &types.ToolCall{
					ID: tc.ID, Name: tc.Name, Args: tc.Args, Position: tc.Position,
				}, tc.Args); intercepted {
					result = r
				}
			}()
			if result != nil {
				break
			}
		}
		a.mu.Unlock()
		if result == nil {
			result = a.execTool(ctx, tc)
		}

		// Run AfterToolCall extensions
		a.mu.Lock()
		for _, ext := range a.extensions {
			func() {
				defer func() {
					if r := recover(); r != nil {
						a.events.Publish(Event{Type: EventError, Error: fmt.Errorf("extension AfterToolCall panic: %v", r)})
					}
				}()
				if next := ext.AfterToolCall(ctx, &types.ToolCall{
					ID:       tc.ID,
					Name:     tc.Name,
					Args:     tc.Args,
					Position: tc.Position,
				}, result); next != nil {
					result = next
				}
			}()
		}
		a.mu.Unlock()

		content := result.Content
		if result.IsError {
			content = "Error: " + content
		}

		// Emit tool output event for TUI rendering
		a.events.Publish(Event{
			Type: EventToolOutput,
			ToolOutput: &types.ToolOutput{
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Content:    content,
				IsError:    result.IsError,
			},
		})

		a.appendMessage(types.Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: tc.ID,
			Timestamp:  time.Now(),
		})
	}
	return true
}

// execTool runs a single tool call.
func (a *Agent) execTool(ctx context.Context, tc *llm.ToolCall) *tools.ToolResult {
	tool, ok := a.tools.Get(tc.Name)
	if !ok {
		return &tools.ToolResult{
			Content: fmt.Sprintf("unknown tool: %s", tc.Name),
			IsError: true,
		}
	}

	if a.dryRun && !tool.IsReadOnly() {
		return &tools.ToolResult{
			Content: fmt.Sprintf("Dry run: would have executed tool %q with args: %s", tc.Name, string(tc.Args)),
		}
	}

	result, err := tool.Execute(ctx, tc.Args, func(partial *tools.ToolResult) {
		a.events.Publish(Event{
			Type:     EventToolDelta,
			Content:  partial.Content,
			ToolCall: &types.ToolCall{ID: tc.ID, Name: tc.Name},
		})
	})
	if err != nil {
		return &tools.ToolResult{
			Content: fmt.Sprintf("tool error: %v", err),
			IsError: true,
		}
	}
	return result
}
