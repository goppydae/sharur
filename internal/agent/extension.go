package agent

import (
	"context"
	"encoding/json"

	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// InputAction controls how ModifyInput's result is applied.
type InputAction string

const (
	// InputContinue passes the original text through unchanged.
	InputContinue InputAction = "continue"
	// InputTransform replaces the user text with InputResult.Text.
	InputTransform InputAction = "transform"
	// InputHandled marks the input as consumed; the message is not appended to the transcript.
	InputHandled InputAction = "handled"
)

// InputResult is returned by ModifyInput to describe how to process the user input.
type InputResult struct {
	Action InputAction
	Text   string
}

// CompactionPrep describes the state passed to BeforeCompact.
type CompactionPrep struct {
	MessageCount    int
	EstimatedTokens int
	PreviousSummary string
}

// CompactionResult can be returned by BeforeCompact to provide a custom summary
// and skip the default LLM-based summarization.
type CompactionResult struct {
	Summary          string
	FirstKeptEntryID string
}

// SessionStartReason identifies why a session is starting.
type SessionStartReason string

const (
	SessionStartNew    SessionStartReason = "new"
	SessionStartResume SessionStartReason = "resume"
)

// SessionEndReason identifies why a session is ending.
type SessionEndReason string

const (
	SessionEndReset SessionEndReason = "reset"
)

// Extension is the unified interface for all extensions (gRPC plugins, Markdown Skills, etc.)
type Extension interface {
	// Name returns the extension's unique identifier.
	Name() string

	// Tools returns additional tools to register with the agent.
	Tools() []tools.Tool

	// BeforePrompt is called before each LLM request.
	// Return a modified state to change the request.
	BeforePrompt(ctx context.Context, state *AgentState) *AgentState

	// BeforeToolCall is called before each tool execution.
	// Return (result, true) to intercept and prevent the tool from running.
	// Return (nil, false) to allow normal execution.
	BeforeToolCall(ctx context.Context, call *ToolCall, args json.RawMessage) (*tools.ToolResult, bool)

	// AfterToolCall is called after each tool call completes.
	// Return a modified result to change the outcome.
	AfterToolCall(ctx context.Context, call *ToolCall, result *tools.ToolResult) *tools.ToolResult

	// ModifySystemPrompt is called to augment the system prompt.
	ModifySystemPrompt(prompt string) string

	// SessionStart is called when a session is attached or the first prompt begins.
	SessionStart(ctx context.Context, sessionID string, reason SessionStartReason)

	// SessionEnd is called when a session is reset or the agent is torn down.
	SessionEnd(ctx context.Context, sessionID string, reason SessionEndReason)

	// AgentStart is called when the agent begins processing a user prompt.
	AgentStart(ctx context.Context)

	// AgentEnd is called when the agent loop finishes (success, error, or abort).
	AgentEnd(ctx context.Context)

	// TurnStart is called at the start of each LLM request turn.
	TurnStart(ctx context.Context)

	// TurnEnd is called after each turn's tool calls have been processed.
	TurnEnd(ctx context.Context)

	// ModifyInput is called with raw user input before it is added to the transcript.
	// Return InputHandled to consume the message without further processing.
	// Return InputTransform to replace the text.
	// Return InputContinue (or zero value) to proceed unchanged.
	ModifyInput(ctx context.Context, text string) InputResult

	// ModifyContext is called with the message slice just before building each LLM
	// request. The returned slice replaces what is sent to the LLM (not the stored
	// transcript). Extensions are chained; each receives the previous result.
	ModifyContext(ctx context.Context, messages []types.Message) []types.Message

	// BeforeProviderRequest is called with the assembled CompletionRequest before
	// it is sent to the LLM provider. Return a modified copy to alter the request.
	BeforeProviderRequest(ctx context.Context, req *llm.CompletionRequest) *llm.CompletionRequest

	// AfterProviderResponse is called after the LLM stream is fully consumed.
	AfterProviderResponse(ctx context.Context, content string, numToolCalls int)

	// BeforeCompact is called before the compaction summarization LLM call.
	// Return a non-nil *CompactionResult to provide a custom summary and skip the
	// default LLM-based summarization entirely.
	BeforeCompact(ctx context.Context, prep CompactionPrep) *CompactionResult

	// AfterCompact is called after compaction completes.
	AfterCompact(ctx context.Context, freedTokens int)
}

// Ensure types compile
var (
	_ Extension = (*NoopExtension)(nil)
)

// NoopExtension is an extension that does nothing — useful as a base embed.
type NoopExtension struct {
	NameStr string
}

func (n *NoopExtension) Name() string        { return n.NameStr }
func (n *NoopExtension) Tools() []tools.Tool { return nil }
func (n *NoopExtension) BeforePrompt(_ context.Context, state *AgentState) *AgentState {
	return state
}
func (n *NoopExtension) BeforeToolCall(_ context.Context, _ *ToolCall, _ json.RawMessage) (*tools.ToolResult, bool) {
	return nil, false
}
func (n *NoopExtension) AfterToolCall(_ context.Context, _ *ToolCall, result *tools.ToolResult) *tools.ToolResult {
	return result
}
func (n *NoopExtension) ModifySystemPrompt(prompt string) string { return prompt }
func (n *NoopExtension) SessionStart(_ context.Context, _ string, _ SessionStartReason)  {}
func (n *NoopExtension) SessionEnd(_ context.Context, _ string, _ SessionEndReason)      {}
func (n *NoopExtension) AgentStart(_ context.Context)                                    {}
func (n *NoopExtension) AgentEnd(_ context.Context)                                      {}
func (n *NoopExtension) TurnStart(_ context.Context)                                     {}
func (n *NoopExtension) TurnEnd(_ context.Context)                                       {}
func (n *NoopExtension) ModifyInput(_ context.Context, _ string) InputResult             { return InputResult{Action: InputContinue} }
func (n *NoopExtension) ModifyContext(_ context.Context, messages []types.Message) []types.Message {
	return messages
}
func (n *NoopExtension) BeforeProviderRequest(_ context.Context, req *llm.CompletionRequest) *llm.CompletionRequest {
	return req
}
func (n *NoopExtension) AfterProviderResponse(_ context.Context, _ string, _ int) {}
func (n *NoopExtension) BeforeCompact(_ context.Context, _ CompactionPrep) *CompactionResult {
	return nil
}
func (n *NoopExtension) AfterCompact(_ context.Context, _ int) {}
