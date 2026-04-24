package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/rpc"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/goppydae/gollm/extensions/proto"
	"github.com/goppydae/gollm/internal/agent"
	"github.com/goppydae/gollm/internal/tools"
)

// HandshakeConfig is the agreed upon handshake for gollm extensions.
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "GOLLM_EXTENSION",
	MagicCookieValue: "v1.0.0",
}

// PluginMap is the map of plugins we can dispense.
var PluginMap = map[string]plugin.Plugin{
	"extension": &ExtensionPlugin{},
}

// ExtensionPlugin is the implementation of plugin.Plugin so we can serve/consume this
// with hashicorp/go-plugin.
type ExtensionPlugin struct {
	// Impl is the actual implementation of the Extension interface.
	// This is only used on the server side (if we wrote a Go extension).
	Impl agent.Extension
}

func (p *ExtensionPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &GRPCServer{Impl: p.Impl}, nil
}

func (p *ExtensionPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return nil, nil // We only use gRPC
}

func (p *ExtensionPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterExtensionServer(s, &GRPCServer{Impl: p.Impl})
	return nil
}

func (p *ExtensionPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCClient{client: proto.NewExtensionClient(c)}, nil
}

// GRPCClient is an implementation of Extension that talks over RPC.
type GRPCClient struct {
	client proto.ExtensionClient
}

func (m *GRPCClient) Name() string {
	resp, err := m.client.Name(context.Background(), &proto.Empty{})
	if err != nil {
		log.Printf("extension Name() RPC error: %v", err)
		return ""
	}
	return resp.Name
}

// Tools queries the extension process for its tool definitions and returns
// RemoteTool wrappers that execute each tool over the ExecuteTool RPC.
func (m *GRPCClient) Tools() []tools.Tool {
	resp, err := m.client.Tools(context.Background(), &proto.Empty{})
	if err != nil {
		log.Printf("extension Tools() RPC error: %v", err)
		return nil
	}
	result := make([]tools.Tool, 0, len(resp.Tools))
	for _, def := range resp.Tools {
		result = append(result, &RemoteTool{
			client:      m.client,
			name:        def.Name,
			description: def.Description,
			schema:      json.RawMessage(def.ParametersJsonSchema),
		})
	}
	return result
}

func (m *GRPCClient) BeforePrompt(ctx context.Context, state *agent.AgentState) *agent.AgentState {
	resp, err := m.client.BeforePrompt(ctx, &proto.BeforePromptRequest{
		State: &proto.AgentState{
			Prompt:        state.SystemPrompt,
			Model:         state.Model,
			Provider:      state.Provider,
			ThinkingLevel: string(state.Thinking),
		},
	})
	if err != nil {
		log.Printf("extension BeforePrompt() RPC error: %v", err)
		return state
	}
	if resp.State == nil {
		return state
	}
	newState := *state
	newState.SystemPrompt = resp.State.Prompt
	if resp.State.Model != "" {
		newState.Model = resp.State.Model
	}
	if resp.State.Provider != "" {
		newState.Provider = resp.State.Provider
	}
	if resp.State.ThinkingLevel != "" {
		newState.Thinking = agent.ThinkingLevel(resp.State.ThinkingLevel)
	}
	return &newState
}

func (m *GRPCClient) AfterToolCall(ctx context.Context, call *agent.ToolCall, result *tools.ToolResult) *tools.ToolResult {
	argsJSON := ""
	if call.Args != nil {
		argsJSON = string(call.Args)
	}
	protoResult := &proto.ToolResult{
		Output: result.Content,
	}
	if result.IsError {
		protoResult.Error = result.Content
		protoResult.Output = ""
	}

	resp, err := m.client.AfterToolCall(ctx, &proto.AfterToolCallRequest{
		Call: &proto.ToolCall{
			Name:          call.Name,
			ArgumentsJson: argsJSON,
		},
		Result: protoResult,
	})
	if err != nil {
		log.Printf("extension AfterToolCall() RPC error: %v", err)
		return result
	}
	if resp.Result == nil {
		return result
	}
	if resp.Result.Error != "" {
		return &tools.ToolResult{Content: resp.Result.Error, IsError: true}
	}
	return &tools.ToolResult{Content: resp.Result.Output}
}

func (m *GRPCClient) ModifySystemPrompt(prompt string) string {
	resp, err := m.client.ModifySystemPrompt(context.Background(), &proto.ModifySystemPromptRequest{
		CurrentPrompt: prompt,
	})
	if err != nil {
		log.Printf("extension ModifySystemPrompt() RPC error: %v", err)
		return prompt
	}
	return resp.ModifiedPrompt
}

// RemoteTool is a tools.Tool that executes over the extension's ExecuteTool gRPC.
type RemoteTool struct {
	client      proto.ExtensionClient
	name        string
	description string
	schema      json.RawMessage
}

func (t *RemoteTool) Name() string             { return t.name }
func (t *RemoteTool) Description() string      { return t.description }
func (t *RemoteTool) Schema() json.RawMessage  { return t.schema }

func (t *RemoteTool) Execute(ctx context.Context, args json.RawMessage, update tools.ToolUpdate) (*tools.ToolResult, error) {
	resp, err := t.client.ExecuteTool(ctx, &proto.ToolCall{
		Name:          t.name,
		ArgumentsJson: string(args),
	})
	if err != nil {
		return nil, fmt.Errorf("remote tool %q: %w", t.name, err)
	}
	result := &tools.ToolResult{
		Content: resp.Output,
		IsError: resp.Error != "",
	}
	if result.IsError {
		result.Content = resp.Error
	}
	if update != nil {
		update(result)
	}
	return result, nil
}

// GRPCServer is the gRPC server that GRPCClient talks to.
type GRPCServer struct {
	proto.UnimplementedExtensionServer
	Impl agent.Extension
}

func (m *GRPCServer) Name(ctx context.Context, req *proto.Empty) (*proto.NameResponse, error) {
	return &proto.NameResponse{Name: m.Impl.Name()}, nil
}

func (m *GRPCServer) Tools(ctx context.Context, req *proto.Empty) (*proto.ToolsResponse, error) {
	implTools := m.Impl.Tools()
	defs := make([]*proto.ToolDefinition, 0, len(implTools))
	for _, t := range implTools {
		defs = append(defs, &proto.ToolDefinition{
			Name:                 t.Name(),
			Description:          t.Description(),
			ParametersJsonSchema: string(t.Schema()),
		})
	}
	return &proto.ToolsResponse{Tools: defs}, nil
}

func (m *GRPCServer) ExecuteTool(ctx context.Context, req *proto.ToolCall) (*proto.ToolResult, error) {
	implTools := m.Impl.Tools()
	for _, t := range implTools {
		if t.Name() != req.Name {
			continue
		}
		result, err := t.Execute(ctx, json.RawMessage(req.ArgumentsJson), nil)
		if err != nil {
			return &proto.ToolResult{Error: err.Error()}, nil
		}
		if result.IsError {
			return &proto.ToolResult{Error: result.Content}, nil
		}
		return &proto.ToolResult{Output: result.Content}, nil
	}
	return &proto.ToolResult{Error: fmt.Sprintf("tool %q not found in extension", req.Name)}, nil
}

func (m *GRPCServer) BeforePrompt(ctx context.Context, req *proto.BeforePromptRequest) (*proto.BeforePromptResponse, error) {
	state := &agent.AgentState{
		SystemPrompt: req.State.Prompt,
		Model:        req.State.Model,
		Provider:     req.State.Provider,
		Thinking:     agent.ThinkingLevel(req.State.ThinkingLevel),
	}
	modified := m.Impl.BeforePrompt(ctx, state)
	return &proto.BeforePromptResponse{
		State: &proto.AgentState{
			Prompt:        modified.SystemPrompt,
			Model:         modified.Model,
			Provider:      modified.Provider,
			ThinkingLevel: string(modified.Thinking),
		},
	}, nil
}

func (m *GRPCServer) AfterToolCall(ctx context.Context, req *proto.AfterToolCallRequest) (*proto.AfterToolCallResponse, error) {
	if req.Call == nil || req.Result == nil {
		return &proto.AfterToolCallResponse{Result: req.Result}, nil
	}
	call := &agent.ToolCall{
		Name: req.Call.Name,
		Args: json.RawMessage(req.Call.ArgumentsJson),
	}
	inResult := &tools.ToolResult{
		Content: req.Result.Output,
		IsError: req.Result.Error != "",
	}
	if inResult.IsError {
		inResult.Content = req.Result.Error
	}
	out := m.Impl.AfterToolCall(ctx, call, inResult)
	protoResult := &proto.ToolResult{Output: out.Content}
	if out.IsError {
		protoResult.Error = out.Content
		protoResult.Output = ""
	}
	return &proto.AfterToolCallResponse{Result: protoResult}, nil
}

func (m *GRPCServer) ModifySystemPrompt(ctx context.Context, req *proto.ModifySystemPromptRequest) (*proto.ModifySystemPromptResponse, error) {
	modified := m.Impl.ModifySystemPrompt(req.CurrentPrompt)
	return &proto.ModifySystemPromptResponse{ModifiedPrompt: modified}, nil
}
