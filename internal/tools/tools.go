// Package tools provides the universal tool interface and registry.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool is the universal tool interface — anything the agent can do.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON Schema for parameters
	Execute(ctx context.Context, args json.RawMessage, update ToolUpdate) (*ToolResult, error)
	IsReadOnly() bool
}

// ToolUpdate is a callback for streaming partial results.
type ToolUpdate func(partial *ToolResult)

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Args     json.RawMessage `json:"args"`
	Position int             `json:"position,omitempty"`
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	Content  string          `json:"content"`
	IsError  bool            `json:"isError,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

// ToolRegistry manages registered tools.
type ToolRegistry struct {
	tools map[string]Tool
}

// NewToolRegistry creates an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns all registered tools.
func (r *ToolRegistry) All() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// Has checks if a tool is registered.
func (r *ToolRegistry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// MustGet retrieves a tool by name, panicking if not found.
// Only call this when the tool's existence has already been guaranteed
// (e.g. during application init). Use Get for runtime lookups.
func (r *ToolRegistry) MustGet(name string) Tool {
	t, ok := r.Get(name)
	if !ok {
		panic(fmt.Sprintf("tool %q not found in registry", name))
	}
	return t
}

// NormalizePath strips a leading '@' from a path if present.
func NormalizePath(path string) string {
	if len(path) > 0 && path[0] == '@' {
		return path[1:]
	}
	return path
}
