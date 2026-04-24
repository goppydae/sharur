package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/goppydae/gollm/internal/types"
)

// OllamaProvider implements the Provider interface for Ollama.
type OllamaProvider struct {
	client    *http.Client
	baseURL   string
	model     string
	maxTokens int
	temp      float64
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		baseURL:   strings.TrimRight(baseURL, "/"),
		model:     model,
		maxTokens: 4096,
		temp:      0.7,
	}
}

// Info returns provider metadata.
func (p *OllamaProvider) Info() ProviderInfo {
	return ProviderInfo{
		Name:          "ollama",
		Model:         p.model,
		MaxTokens:     p.maxTokens,
		ContextWindow: 4096, // default for Ollama if unknown
		HasToolCall:   true,
		HasImages:     true,
	}
}

// SetModel sets the model name.
func (p *OllamaProvider) SetModel(model string) {
	p.model = model
}

// SetMaxTokens sets the max tokens.
func (p *OllamaProvider) SetMaxTokens(n int) {
	p.maxTokens = n
}

// SetTemperature sets the temperature.
func (p *OllamaProvider) SetTemperature(t float64) {
	p.temp = t
}

// Stream sends messages and returns an event stream.
func (p *OllamaProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan *Event, error) {
	events := make(chan *Event, 32)

	go func() {
		defer close(events)

		reqBody := map[string]any{
			"model":    p.model,
			"messages": req.Messages,
			"stream":   true,
		}

		if req.System != "" {
			reqBody["system"] = req.System
		}

		if len(req.Tools) > 0 {
			tools := make([]map[string]any, len(req.Tools))
			for i, t := range req.Tools {
				tools[i] = map[string]any{
					"type":        "function",
					"function":    map[string]any{"name": t.Name, "description": t.Description, "parameters": json.RawMessage(t.Schema)},
				}
			}
			reqBody["tools"] = tools
		}

		if req.MaxTokens > 0 {
			reqBody["num_predict"] = req.MaxTokens
		} else if p.maxTokens > 0 {
			reqBody["num_predict"] = p.maxTokens
		}

		if req.Temperature > 0 {
			reqBody["temperature"] = req.Temperature
		} else if p.temp > 0 {
			reqBody["temperature"] = p.temp
		}

		// Map thinking level
		if req.Thinking != types.ThinkingOff {
			reqBody["num_ctx"] = 8192 // Enable longer context for thinking
		}

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			events <- &Event{Type: EventError, Error: fmt.Errorf("marshal request: %w", err)}
			return
		}

		url := p.baseURL + "/api/chat"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
		if err != nil {
			events <- &Event{Type: EventError, Error: fmt.Errorf("create request: %w", err)}
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			events <- &Event{Type: EventError, Error: fmt.Errorf("request: %w", err)}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			events <- &Event{Type: EventError, Error: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))}
			return
		}

		// Stream response
		decoder := json.NewDecoder(resp.Body)
		var fullContent strings.Builder

		for decoder.More() {
			var chunk map[string]any
			if err := decoder.Decode(&chunk); err != nil {
				events <- &Event{Type: EventError, Error: fmt.Errorf("decode chunk: %w", err)}
				return
			}

			done, _ := chunk["done"].(bool)
			_ = done

			if content, ok := chunk["message"].(map[string]any); ok {
				if text, textOk := content["content"].(string); textOk {
					fullContent.WriteString(text)
					events <- &Event{
						Type:    EventTextDelta,
						Content: text,
					}
				}

				if toolList, ok := content["tool_calls"].([]any); ok {
					for i, tc := range toolList {
						tcMap, ok := tc.(map[string]any)
						if !ok {
							continue
						}
						funcMap, ok := tcMap["function"].(map[string]any)
						if !ok {
							continue
						}
						name, _ := funcMap["name"].(string)
						argsStr, _ := funcMap["arguments"].(string)
						args := json.RawMessage(argsStr)

						id, _ := tcMap["id"].(string)
						if id == "" {
							id = fmt.Sprintf("call_%d", i)
						}

						toolCall := ToolCall{
							ID:       id,
							Name:     name,
							Args:     args,
							Position: i,
						}
						events <- &Event{
							Type:     EventToolCall,
							ToolCall: &toolCall,
						}
					}
				}
			}

			if rawUsage, ok := chunk["eval_count"].(float64); ok {
				if promptCount, ok2 := chunk["prompt_eval_count"].(float64); ok2 {
					events <- &Event{
						Type: EventMessageEnd,
						Usage: &Usage{
							PromptTokens:     int(promptCount),
							CompletionTokens: int(rawUsage),
							TotalTokens:      int(promptCount + rawUsage),
						},
					}
				}
			}
		}

		// Final event
		events <- &Event{
			Type:    EventMessageEnd,
			Content: fullContent.String(),
			ToolCall: nil, // Multiple tool calls are in the stream
		}
	}()

	return events, nil
}

// ListModels implements ModelLister by querying the Ollama /api/tags endpoint.
func (p *OllamaProvider) ListModels() ([]string, error) {
	resp, err := p.client.Get(p.baseURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}
	names := make([]string, len(payload.Models))
	for i, m := range payload.Models {
		names[i] = m.Name
	}
	return names, nil
}
