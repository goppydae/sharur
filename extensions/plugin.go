package extensions

import (
	"context"
	"encoding/json"

	"github.com/goppydae/gollm/internal/agent"
)

// ToolCall describes a tool invocation passed to Plugin hook methods.
type ToolCall struct {
	Name string
	Args json.RawMessage
}

// ToolResult is the outcome of a tool call or an interception.
type ToolResult struct {
	Content string
	IsError bool
}

// AgentState is the mutable prompt state passed to BeforePrompt.
type AgentState struct {
	SystemPrompt  string
	Model         string
	Provider      string
	ThinkingLevel string
}

// ToolDefinition describes a tool contributed by a Plugin.
type ToolDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
	IsReadOnly  bool
}

// Plugin is the interface that standalone gRPC extension binaries implement.
// Embed NoopPlugin and override only the methods you need.
type Plugin interface {
	Name() string
	Tools() []ToolDefinition
	ExecuteTool(ctx context.Context, name string, args json.RawMessage) ToolResult
	BeforePrompt(ctx context.Context, state AgentState) AgentState
	BeforeToolCall(ctx context.Context, call ToolCall, args json.RawMessage) (ToolResult, bool)
	AfterToolCall(ctx context.Context, call ToolCall, result ToolResult) ToolResult
	ModifySystemPrompt(prompt string) string

	SessionStart(ctx context.Context, sessionID string, reason agent.SessionStartReason)
	SessionEnd(ctx context.Context, sessionID string, reason agent.SessionEndReason)
	AgentStart(ctx context.Context)
	AgentEnd(ctx context.Context)
	TurnStart(ctx context.Context)
	TurnEnd(ctx context.Context)
	ModifyInput(ctx context.Context, text string) agent.InputResult
	ModifyContext(ctx context.Context, messagesJSON string) string
	BeforeProviderRequest(ctx context.Context, requestJSON string) string
	AfterProviderResponse(ctx context.Context, content string, numToolCalls int)
	BeforeCompact(ctx context.Context, prep agent.CompactionPrep) *agent.CompactionResult
	AfterCompact(ctx context.Context, freedTokens int)
}

// Compile-time check.
var _ Plugin = (*NoopPlugin)(nil)

// NoopPlugin is a base Plugin implementation with no-op defaults.
// Embed it in your Plugin struct and override only what you need.
type NoopPlugin struct {
	NameStr string
}

func (n *NoopPlugin) Name() string            { return n.NameStr }
func (n *NoopPlugin) Tools() []ToolDefinition { return nil }
func (n *NoopPlugin) ExecuteTool(_ context.Context, name string, _ json.RawMessage) ToolResult {
	return ToolResult{Content: "tool not found: " + name, IsError: true}
}
func (n *NoopPlugin) BeforePrompt(_ context.Context, state AgentState) AgentState { return state }
func (n *NoopPlugin) BeforeToolCall(_ context.Context, _ ToolCall, _ json.RawMessage) (ToolResult, bool) {
	return ToolResult{}, false
}
func (n *NoopPlugin) AfterToolCall(_ context.Context, _ ToolCall, result ToolResult) ToolResult {
	return result
}
func (n *NoopPlugin) ModifySystemPrompt(prompt string) string                              { return prompt }
func (n *NoopPlugin) SessionStart(_ context.Context, _ string, _ agent.SessionStartReason) {}
func (n *NoopPlugin) SessionEnd(_ context.Context, _ string, _ agent.SessionEndReason)     {}
func (n *NoopPlugin) AgentStart(_ context.Context)                                         {}
func (n *NoopPlugin) AgentEnd(_ context.Context)                                           {}
func (n *NoopPlugin) TurnStart(_ context.Context)                                          {}
func (n *NoopPlugin) TurnEnd(_ context.Context)                                            {}
func (n *NoopPlugin) ModifyInput(_ context.Context, _ string) agent.InputResult {
	return agent.InputResult{Action: agent.InputContinue}
}
func (n *NoopPlugin) ModifyContext(_ context.Context, messagesJSON string) string           { return messagesJSON }
func (n *NoopPlugin) BeforeProviderRequest(_ context.Context, requestJSON string) string    { return requestJSON }
func (n *NoopPlugin) AfterProviderResponse(_ context.Context, _ string, _ int)             {}
func (n *NoopPlugin) BeforeCompact(_ context.Context, _ agent.CompactionPrep) *agent.CompactionResult {
	return nil
}
func (n *NoopPlugin) AfterCompact(_ context.Context, _ int) {}
