package interactive

import (
	"context"

	pb "github.com/goppydae/gollm/internal/gen/gollm/v1"
	"google.golang.org/grpc"
)

var _ pb.AgentServiceClient = (*stubClient)(nil)

type stubClient struct{}

func (s *stubClient) Prompt(ctx context.Context, in *pb.PromptRequest, opts ...grpc.CallOption) (pb.AgentService_PromptClient, error) {
	return nil, nil
}
func (s *stubClient) Steer(ctx context.Context, in *pb.SteerRequest, opts ...grpc.CallOption) (*pb.SteerResponse, error) {
	return &pb.SteerResponse{Ok: true}, nil
}
func (s *stubClient) Abort(ctx context.Context, in *pb.AbortRequest, opts ...grpc.CallOption) (*pb.AbortResponse, error) {
	return &pb.AbortResponse{Ok: true}, nil
}
func (s *stubClient) NewSession(ctx context.Context, in *pb.NewSessionRequest, opts ...grpc.CallOption) (*pb.NewSessionResponse, error) {
	return &pb.NewSessionResponse{SessionId: "test-session"}, nil
}
func (s *stubClient) DeleteSession(ctx context.Context, in *pb.DeleteSessionRequest, opts ...grpc.CallOption) (*pb.DeleteSessionResponse, error) {
	return &pb.DeleteSessionResponse{Ok: true}, nil
}
func (s *stubClient) ListSessions(ctx context.Context, in *pb.ListSessionsRequest, opts ...grpc.CallOption) (*pb.ListSessionsResponse, error) {
	return &pb.ListSessionsResponse{}, nil
}
func (s *stubClient) GetState(ctx context.Context, in *pb.GetStateRequest, opts ...grpc.CallOption) (*pb.GetStateResponse, error) {
	return &pb.GetStateResponse{SessionId: in.SessionId}, nil
}
func (s *stubClient) GetMessages(ctx context.Context, in *pb.GetMessagesRequest, opts ...grpc.CallOption) (*pb.GetMessagesResponse, error) {
	return &pb.GetMessagesResponse{}, nil
}
func (s *stubClient) ConfigureSession(ctx context.Context, in *pb.ConfigureSessionRequest, opts ...grpc.CallOption) (*pb.ConfigureSessionResponse, error) {
	return &pb.ConfigureSessionResponse{Ok: true}, nil
}
func (s *stubClient) SetModel(ctx context.Context, in *pb.SetModelRequest, opts ...grpc.CallOption) (*pb.SetModelResponse, error) {
	return &pb.SetModelResponse{Ok: true}, nil
}
func (s *stubClient) SetThinkingLevel(ctx context.Context, in *pb.SetThinkingLevelRequest, opts ...grpc.CallOption) (*pb.SetThinkingLevelResponse, error) {
	return &pb.SetThinkingLevelResponse{Ok: true}, nil
}
func (s *stubClient) SetSessionName(ctx context.Context, in *pb.SetSessionNameRequest, opts ...grpc.CallOption) (*pb.SetSessionNameResponse, error) {
	return &pb.SetSessionNameResponse{Ok: true}, nil
}
func (s *stubClient) Compact(ctx context.Context, in *pb.CompactRequest, opts ...grpc.CallOption) (*pb.CompactResponse, error) {
	return &pb.CompactResponse{Ok: true}, nil
}
func (s *stubClient) BranchSession(ctx context.Context, in *pb.BranchSessionRequest, opts ...grpc.CallOption) (*pb.NewSessionResponse, error) {
	return &pb.NewSessionResponse{SessionId: "branch-session"}, nil
}
func (s *stubClient) ForkSession(ctx context.Context, in *pb.ForkSessionRequest, opts ...grpc.CallOption) (*pb.NewSessionResponse, error) {
	return &pb.NewSessionResponse{SessionId: "fork-session"}, nil
}
func (s *stubClient) RebaseSession(ctx context.Context, in *pb.RebaseSessionRequest, opts ...grpc.CallOption) (*pb.NewSessionResponse, error) {
	return &pb.NewSessionResponse{SessionId: "rebase-session"}, nil
}
func (s *stubClient) MergeSession(ctx context.Context, in *pb.MergeSessionRequest, opts ...grpc.CallOption) (*pb.NewSessionResponse, error) {
	return &pb.NewSessionResponse{SessionId: "merge-session"}, nil
}
func (s *stubClient) GetSessionTree(ctx context.Context, in *pb.GetSessionTreeRequest, opts ...grpc.CallOption) (*pb.GetSessionTreeResponse, error) {
	return &pb.GetSessionTreeResponse{}, nil
}
func (s *stubClient) FollowUp(ctx context.Context, in *pb.FollowUpRequest, opts ...grpc.CallOption) (*pb.FollowUpResponse, error) {
	return &pb.FollowUpResponse{Ok: true}, nil
}
func (s *stubClient) StreamEvents(ctx context.Context, in *pb.StreamEventsRequest, opts ...grpc.CallOption) (pb.AgentService_StreamEventsClient, error) {
	return nil, nil
}
