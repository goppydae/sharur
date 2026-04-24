package modes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/llm"
	"github.com/goppydae/gollm/internal/session"
	"github.com/goppydae/gollm/internal/tools"
	"github.com/goppydae/gollm/internal/types"
)

// NewRPCHandler creates an RPC mode handler.
func NewRPCHandler(provider llm.Provider, registry *tools.ToolRegistry, mgr *session.Manager, exts []agent.Extension) Handler {
	return &RPCHandler{
		Provider:   provider,
		Registry:   registry,
		Manager:    mgr,
		Extensions: exts,
	}
}

// RPCHandler handles RPC mode (stdin/stdout JSONL protocol).
type RPCHandler struct {
	Provider   llm.Provider
	Registry   *tools.ToolRegistry
	Manager    *session.Manager
	Extensions []agent.Extension
}

func (h *RPCHandler) Run(args []string) error {
	ag := agent.New(h.Provider, h.Registry)
	ag.SetExtensions(h.Extensions)

	ag.Subscribe(func(e agent.Event) {
		switch e.Type {
		case agent.EventMessageStart:
			writeJSON(os.Stdout, map[string]any{"type": "message_start"})
		case agent.EventTextDelta:
			writeJSON(os.Stdout, map[string]any{
				"type":    "text_delta",
				"content": e.Content,
			})
		case agent.EventToolCall:
			writeJSON(os.Stdout, map[string]any{
				"type":     "tool_call",
				"toolCall": formatToolCall(e.ToolCall),
			})
		case agent.EventMessageEnd:
			ev := map[string]any{"type": "message_end"}
			if e.Usage != nil {
				ev["usage"] = e.Usage
			}
			writeJSON(os.Stdout, ev)
		case agent.EventAgentStart:
			writeJSON(os.Stdout, map[string]any{"type": "agent_start"})
		case agent.EventAgentEnd:
			writeJSON(os.Stdout, map[string]any{"type": "agent_end"})
		case agent.EventTurnStart:
			writeJSON(os.Stdout, map[string]any{"type": "turn_start"})
		case agent.EventAbort:
			writeJSON(os.Stdout, map[string]any{"type": "abort"})
		case agent.EventStateChange:
			if e.StateChange != nil {
				writeJSON(os.Stdout, map[string]any{
					"type": "state_change",
					"from": string(e.StateChange.From),
					"to":   string(e.StateChange.To),
				})
			}
		case agent.EventError:
			msg := ""
			if e.Error != nil {
				msg = e.Error.Error()
			}
			writeJSON(os.Stderr, map[string]any{"type": "error", "error": msg})
		}
	})

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var cmd struct {
			ID           int64  `json:"id"`
			Type         string `json:"type"`
			Message      string `json:"message"`
			Model        string `json:"model"`
			ThinkingLevel string `json:"thinkingLevel"`
			Name         string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			writeJSON(os.Stdout, map[string]any{
				"id":      0,
				"type":    "response",
				"success": false,
				"error":   fmt.Sprintf("parse error: %v", err),
			})
			continue
		}

		respond := func(extra map[string]any) {
			if cmd.ID == 0 {
				return
			}
			msg := map[string]any{
				"id":      cmd.ID,
				"type":    "response",
				"command": cmd.Type,
				"success": true,
			}
			for k, v := range extra {
				msg[k] = v
			}
			writeJSON(os.Stdout, msg)
		}

		respondErr := func(err string) {
			if cmd.ID == 0 {
				return
			}
			writeJSON(os.Stdout, map[string]any{
				"id":      cmd.ID,
				"type":    "response",
				"command": cmd.Type,
				"success": false,
				"error":   err,
			})
		}

		switch cmd.Type {
		case "prompt":
			respond(nil)
			if err := ag.Prompt(context.Background(), cmd.Message); err != nil {
				respondErr(err.Error())
			}

		case "steer":
			// Send a follow-up mid-turn correction (same as prompt here).
			respond(nil)
			if err := ag.Prompt(context.Background(), cmd.Message); err != nil {
				respondErr(err.Error())
			}

		case "follow_up":
			respond(nil)
			if err := ag.Prompt(context.Background(), cmd.Message); err != nil {
				respondErr(err.Error())
			}

		case "abort":
			ag.Abort()
			respond(nil)

		case "new_session":
			ag.Reset()
			if cmd.Name != "" {
				ag.SetSessionName(cmd.Name)
			}
			respond(nil)

		case "get_state":
			respond(map[string]any{"data": ag.State()})

		case "get_messages":
			respond(map[string]any{"data": ag.Messages()})

		case "set_model":
			ag.SetModel(cmd.Model)
			respond(nil)

		case "set_thinking_level":
			ag.SetThinkingLevel(types.ThinkingLevel(cmd.ThinkingLevel))
			respond(nil)

		case "set_session_name":
			ag.SetSessionName(cmd.Name)
			respond(nil)

		case "compact":
			ag.Compact(20000)
			respond(nil)

		case "bash":
			// Convenience: run a bash command outside the agent loop.
			result := runBashDirect(cmd.Message)
			respond(map[string]any{"data": result})

		default:
			respondErr(fmt.Sprintf("unknown command: %s", cmd.Type))
		}
	}

	return scanner.Err()
}



func runBashDirect(command string) map[string]any {
	if command == "" {
		return map[string]any{"error": "empty command"}
	}
	t := tools.Bash{}
	result, err := t.Execute(context.Background(), mustMarshal(map[string]any{"command": command}), nil)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{
		"output":   result.Content,
		"isError":  result.IsError,
		"metadata": result.Metadata,
	}
}


