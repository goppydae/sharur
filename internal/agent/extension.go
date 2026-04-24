package agent

import (
	"context"

	"github.com/goppydae/gollm/internal/tools"
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

	// AfterToolCall is called after each tool call completes.
	// Return a modified result to change the outcome.
	AfterToolCall(ctx context.Context, call *ToolCall, result *tools.ToolResult) *tools.ToolResult

	// ModifySystemPrompt is called to augment the system prompt.
	ModifySystemPrompt(prompt string) string
}

// Ensure types compile
var (
	_ Extension = (*NoopExtension)(nil)
)

// NoopExtension is an extension that does nothing — useful as a base embed.
type NoopExtension struct {
	NameStr string
}

func (n *NoopExtension) Name() string                                    { return n.NameStr }
func (n *NoopExtension) Tools() []tools.Tool                             { return nil }
func (n *NoopExtension) BeforePrompt(ctx context.Context, state *AgentState) *AgentState { return state }
func (n *NoopExtension) AfterToolCall(ctx context.Context, call *ToolCall, result *tools.ToolResult) *tools.ToolResult { return result }
func (n *NoopExtension) ModifySystemPrompt(prompt string) string         { return prompt }
