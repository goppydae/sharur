package extensions

import (
	"context"
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
		return ""
	}
	return resp.Name
}

func (m *GRPCClient) Tools() []tools.Tool {
	// For now, returning an empty list. Dynamic tool loading from Python
	// requires converting the proto.ToolDefinition back to a generic tools.Tool.
	// Since tools usually execute code natively, an extension tool would need
	// to marshal executions back over gRPC. We will leave this stubbed out for now
	// as complex tool marshaling is outside the immediate scope, but the proto allows it.
	return nil
}

func (m *GRPCClient) BeforePrompt(ctx context.Context, state *agent.AgentState) *agent.AgentState {
	resp, err := m.client.BeforePrompt(ctx, &proto.BeforePromptRequest{
		State: &proto.AgentState{
			Prompt: state.SystemPrompt, // simplified mapping
		},
	})
	if err != nil || resp.State == nil {
		return state
	}
	
	// Create a new state based on the modified one
	newState := *state
	newState.SystemPrompt = resp.State.Prompt
	return &newState
}

func (m *GRPCClient) AfterToolCall(ctx context.Context, call *agent.ToolCall, result *tools.ToolResult) *tools.ToolResult {
	// Simplified stub for now
	return result
}

func (m *GRPCClient) ModifySystemPrompt(prompt string) string {
	resp, err := m.client.ModifySystemPrompt(context.Background(), &proto.ModifySystemPromptRequest{
		CurrentPrompt: prompt,
	})
	if err != nil {
		return prompt
	}
	return resp.ModifiedPrompt
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
	return &proto.ToolsResponse{}, nil
}

func (m *GRPCServer) BeforePrompt(ctx context.Context, req *proto.BeforePromptRequest) (*proto.BeforePromptResponse, error) {
	// Stub implementation mapping
	state := &agent.AgentState{SystemPrompt: req.State.Prompt}
	modified := m.Impl.BeforePrompt(ctx, state)
	return &proto.BeforePromptResponse{
		State: &proto.AgentState{
			Prompt: modified.SystemPrompt,
		},
	}, nil
}

func (m *GRPCServer) AfterToolCall(ctx context.Context, req *proto.AfterToolCallRequest) (*proto.AfterToolCallResponse, error) {
	return &proto.AfterToolCallResponse{}, nil
}

func (m *GRPCServer) ModifySystemPrompt(ctx context.Context, req *proto.ModifySystemPromptRequest) (*proto.ModifySystemPromptResponse, error) {
	modified := m.Impl.ModifySystemPrompt(req.CurrentPrompt)
	return &proto.ModifySystemPromptResponse{ModifiedPrompt: modified}, nil
}
