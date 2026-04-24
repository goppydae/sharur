package modes

import (
	"encoding/json"
	"os"

	"github.com/goppydae/gollm/internal/agent"
)

func formatToolCall(tc *agent.ToolCall) map[string]any {
	if tc == nil {
		return nil
	}
	var args json.RawMessage
	if len(tc.Args) > 0 {
		args = tc.Args
	}
	return map[string]any{
		"id":       tc.ID,
		"name":     tc.Name,
		"args":     args,
		"position": tc.Position,
	}
}

func writeJSON(w *os.File, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	b = append(b, '\n')
	w.Write(b) //nolint:errcheck
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
