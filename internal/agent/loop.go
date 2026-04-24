package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// runTurn is the core agentic loop: prompt → LLM → tools → LLM → ... until no more tool calls.
func (a *Agent) runTurn(ctx context.Context) {
	defer func() {
		_ = a.lifeState.Transition(StateIdle)
		a.events.Publish(Event{Type: EventAgentEnd})
		close(a.done)
	}()

	for {
		// 1. Drain queues before next LLM call (or turn completion)
		if a.drainQueues() {
			continue
		}

		_ = a.lifeState.Transition(StateThinking)
		
		// Run BeforePrompt extensions
		a.mu.Lock()
		for _, ext := range a.extensions {
			a.state = ext.BeforePrompt(ctx, a.state)
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

		content, thinking, llmCalls, ok := a.consumeStream(ctx, stream)
		if !ok {
			return
		}

		// Append assistant message with any tool calls
		assistantMsg := types.Message{Role: "assistant", Content: content, Thinking: thinking}
		for _, tc := range llmCalls {
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, types.ToolCall{
				ID:       tc.ID,
				Name:     tc.Name,
				Args:     tc.Args,
				Position: tc.Position,
			})
		}
		a.mu.Lock()
		a.state.Messages = append(a.state.Messages, assistantMsg)
		a.mu.Unlock()

		a.events.Publish(Event{Type: EventMessageEnd})

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
		a.mu.Lock()
		a.state.Messages = append(a.state.Messages, msg)
		a.mu.Unlock()
		a.events.Publish(Event{Type: EventMessageEnd})
	}
	
	return true
}

// buildRequest snapshots current state into a CompletionRequest.
func (a *Agent) buildRequest() *llm.CompletionRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()

	msgs := make([]types.Message, len(a.state.Messages))
	copy(msgs, a.state.Messages)

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
func (a *Agent) consumeStream(ctx context.Context, stream <-chan *llm.Event) (string, string, []*llm.ToolCall, bool) {
	var sb strings.Builder
	var thinkingSb strings.Builder
	var toolCalls []*llm.ToolCall

	for {
		select {
		case <-a.stop:
			_ = a.lifeState.Transition(StateAborting)
			a.events.Publish(Event{Type: EventAbort})
			return "", "", nil, false
		case <-ctx.Done():
			_ = a.lifeState.Transition(StateAborting)
			a.events.Publish(Event{Type: EventAbort})
			return "", "", nil, false
		case ev, ok := <-stream:
			if !ok {
				return sb.String(), thinkingSb.String(), toolCalls, true
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
				if ev.Usage != nil {
					a.events.Publish(Event{Type: EventMessageEnd, Usage: ev.Usage})
					a.events.Publish(Event{Type: EventTokens, Value: int64(ev.Usage.TotalTokens)})
				}
			case llm.EventError:
				_ = a.lifeState.Transition(StateError)
				a.events.Publish(Event{Type: EventError, Error: ev.Error})
				return "", "", nil, false
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

		result := a.execTool(ctx, tc)
		
		// Run AfterToolCall extensions
		a.mu.Lock()
		for _, ext := range a.extensions {
			result = ext.AfterToolCall(ctx, &types.ToolCall{
				ID:       tc.ID,
				Name:     tc.Name,
				Args:     tc.Args,
				Position: tc.Position,
			}, result)
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

		a.mu.Lock()
		a.state.Messages = append(a.state.Messages, types.Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: tc.ID,
		})
		a.mu.Unlock()
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

	result, err := tool.Execute(ctx, tc.Args, func(partial *tools.ToolResult) {
		a.events.Publish(Event{
			Type:    EventToolDelta,
			Content: partial.Content,
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
