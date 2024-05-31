// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             v5.26.1
// source: proto/service.proto

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	ConsensusApi_Echo_FullMethodName              = "/consensus.ConsensusApi/Echo"
	ConsensusApi_GetValidatorInfo_FullMethodName  = "/consensus.ConsensusApi/GetValidatorInfo"
	ConsensusApi_GetValidatorState_FullMethodName = "/consensus.ConsensusApi/GetValidatorState"
	ConsensusApi_InitTransaction_FullMethodName   = "/consensus.ConsensusApi/InitTransaction"
)

// ConsensusApiClient is the client API for ConsensusApi service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type ConsensusApiClient interface {
	Echo(ctx context.Context, in *RequestEcho, opts ...grpc.CallOption) (*ResponseEcho, error)
	GetValidatorInfo(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*ValidatorInfo, error)
	GetValidatorState(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*ValidatorState, error)
	InitTransaction(ctx context.Context, opts ...grpc.CallOption) (ConsensusApi_InitTransactionClient, error)
}

type consensusApiClient struct {
	cc grpc.ClientConnInterface
}

func NewConsensusApiClient(cc grpc.ClientConnInterface) ConsensusApiClient {
	return &consensusApiClient{cc}
}

func (c *consensusApiClient) Echo(ctx context.Context, in *RequestEcho, opts ...grpc.CallOption) (*ResponseEcho, error) {
	out := new(ResponseEcho)
	err := c.cc.Invoke(ctx, ConsensusApi_Echo_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *consensusApiClient) GetValidatorInfo(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*ValidatorInfo, error) {
	out := new(ValidatorInfo)
	err := c.cc.Invoke(ctx, ConsensusApi_GetValidatorInfo_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *consensusApiClient) GetValidatorState(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*ValidatorState, error) {
	out := new(ValidatorState)
	err := c.cc.Invoke(ctx, ConsensusApi_GetValidatorState_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *consensusApiClient) InitTransaction(ctx context.Context, opts ...grpc.CallOption) (ConsensusApi_InitTransactionClient, error) {
	stream, err := c.cc.NewStream(ctx, &ConsensusApi_ServiceDesc.Streams[0], ConsensusApi_InitTransaction_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &consensusApiInitTransactionClient{stream}
	return x, nil
}

type ConsensusApi_InitTransactionClient interface {
	Send(*ExternalTransaction) error
	Recv() (*CommitedTransactions, error)
	grpc.ClientStream
}

type consensusApiInitTransactionClient struct {
	grpc.ClientStream
}

func (x *consensusApiInitTransactionClient) Send(m *ExternalTransaction) error {
	return x.ClientStream.SendMsg(m)
}

func (x *consensusApiInitTransactionClient) Recv() (*CommitedTransactions, error) {
	m := new(CommitedTransactions)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// ConsensusApiServer is the server API for ConsensusApi service.
// All implementations must embed UnimplementedConsensusApiServer
// for forward compatibility
type ConsensusApiServer interface {
	Echo(context.Context, *RequestEcho) (*ResponseEcho, error)
	GetValidatorInfo(context.Context, *emptypb.Empty) (*ValidatorInfo, error)
	GetValidatorState(context.Context, *emptypb.Empty) (*ValidatorState, error)
	InitTransaction(ConsensusApi_InitTransactionServer) error
	mustEmbedUnimplementedConsensusApiServer()
}

// UnimplementedConsensusApiServer must be embedded to have forward compatible implementations.
type UnimplementedConsensusApiServer struct {
}

func (UnimplementedConsensusApiServer) Echo(context.Context, *RequestEcho) (*ResponseEcho, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Echo not implemented")
}
func (UnimplementedConsensusApiServer) GetValidatorInfo(context.Context, *emptypb.Empty) (*ValidatorInfo, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetValidatorInfo not implemented")
}
func (UnimplementedConsensusApiServer) GetValidatorState(context.Context, *emptypb.Empty) (*ValidatorState, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetValidatorState not implemented")
}
func (UnimplementedConsensusApiServer) InitTransaction(ConsensusApi_InitTransactionServer) error {
	return status.Errorf(codes.Unimplemented, "method InitTransaction not implemented")
}
func (UnimplementedConsensusApiServer) mustEmbedUnimplementedConsensusApiServer() {}

// UnsafeConsensusApiServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to ConsensusApiServer will
// result in compilation errors.
type UnsafeConsensusApiServer interface {
	mustEmbedUnimplementedConsensusApiServer()
}

func RegisterConsensusApiServer(s grpc.ServiceRegistrar, srv ConsensusApiServer) {
	s.RegisterService(&ConsensusApi_ServiceDesc, srv)
}

func _ConsensusApi_Echo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RequestEcho)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConsensusApiServer).Echo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: ConsensusApi_Echo_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConsensusApiServer).Echo(ctx, req.(*RequestEcho))
	}
	return interceptor(ctx, in, info, handler)
}

func _ConsensusApi_GetValidatorInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConsensusApiServer).GetValidatorInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: ConsensusApi_GetValidatorInfo_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConsensusApiServer).GetValidatorInfo(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _ConsensusApi_GetValidatorState_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(emptypb.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConsensusApiServer).GetValidatorState(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: ConsensusApi_GetValidatorState_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConsensusApiServer).GetValidatorState(ctx, req.(*emptypb.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _ConsensusApi_InitTransaction_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(ConsensusApiServer).InitTransaction(&consensusApiInitTransactionServer{stream})
}

type ConsensusApi_InitTransactionServer interface {
	Send(*CommitedTransactions) error
	Recv() (*ExternalTransaction, error)
	grpc.ServerStream
}

type consensusApiInitTransactionServer struct {
	grpc.ServerStream
}

func (x *consensusApiInitTransactionServer) Send(m *CommitedTransactions) error {
	return x.ServerStream.SendMsg(m)
}

func (x *consensusApiInitTransactionServer) Recv() (*ExternalTransaction, error) {
	m := new(ExternalTransaction)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// ConsensusApi_ServiceDesc is the grpc.ServiceDesc for ConsensusApi service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var ConsensusApi_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "consensus.ConsensusApi",
	HandlerType: (*ConsensusApiServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Echo",
			Handler:    _ConsensusApi_Echo_Handler,
		},
		{
			MethodName: "GetValidatorInfo",
			Handler:    _ConsensusApi_GetValidatorInfo_Handler,
		},
		{
			MethodName: "GetValidatorState",
			Handler:    _ConsensusApi_GetValidatorState_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "InitTransaction",
			Handler:       _ConsensusApi_InitTransaction_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "proto/service.proto",
}
