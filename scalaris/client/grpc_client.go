package client

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	cmtnet "github.com/cometbft/cometbft/libs/net"
	"github.com/cometbft/cometbft/libs/service"
	consensus "github.com/cometbft/cometbft/scalaris/consensus/proto"
)

var _ Client = (*grpcClient)(nil)

// A stripped copy of the remoteClient that makes
// synchronous calls using grpc.
type grpcClient struct {
	service.BaseService
	mustConnect bool

	client   consensus.ConsensusApiClient
	conn     *grpc.ClientConn
	chReqRes chan *ReqRes // dispatches "async" responses to callbacks *in order*, needed by mempool

	mtx   sync.Mutex
	addr  string
	err   error
	resCb func(*consensus.Request, *consensus.Response) // listens to all callbacks
}

func NewGRPCClient(addr string, mustConnect bool) Client {
	cli := &grpcClient{
		addr:        addr,
		mustConnect: mustConnect,
		// Buffering the channel is needed to make calls appear asynchronous,
		// which is required when the caller makes multiple async calls before
		// processing callbacks (e.g. due to holding locks). 64 means that a
		// caller can make up to 64 async calls before a callback must be
		// processed (otherwise it deadlocks). It also means that we can make 64
		// gRPC calls while processing a slow callback at the channel head.
		chReqRes: make(chan *ReqRes, 64),
	}
	cli.BaseService = *service.NewBaseService(nil, "grpcClient", cli)
	return cli
}

func dialerFunc(_ context.Context, addr string) (net.Conn, error) {
	return cmtnet.Connect(addr)
}

// runSendTransaction sends a bunch of transactions to the server
// for testing purposes
func runSendTransaction(client consensus.ConsensusApiClient) {
	println("InitTransaction start")
	notes := []*consensus.ExternalTransaction{
		{Namespace: "1", TxBytes: []byte("1")},
		{Namespace: "2", TxBytes: []byte("2")},
		{Namespace: "3", TxBytes: []byte("3")},
		{Namespace: "4", TxBytes: []byte("4")},
		{Namespace: "5", TxBytes: []byte("5")},
		{Namespace: "6", TxBytes: []byte("6")},
		{Namespace: "7", TxBytes: []byte("7")},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.InitTransaction(ctx)
	println("InitTransaction end inside")
	if err != nil {
		println("client.InitTransaction failed: %v", err)
		println("client.InitTransaction failed: %v", err)
	}
	waitc := make(chan struct{})
	go func() {
		for {
			in, err := stream.Recv()
			if err == io.EOF {
				// read done.
				close(waitc)
				return
			}
			if err != nil {
				println("client.InitTransaction failed: %v", err)
			}
			println("Got transactions %s", in.Transactions)
		}
	}()
	println("Sending transactions")
	for _, note := range notes {
		if err := stream.Send(note); err != nil {
			log.Fatalf("client.InitTransaction: stream.Send(%v) failed: %v", note, err)
		}
	}
	println("Closing stream")

	stream.CloseSend()
	<-waitc
}

func (cli *grpcClient) Start() error {
	cli.BaseService.Logger.Info("Starting abci.grpcClient", "addr", cli.addr)
	println("Starting abci.grpcClient", "addr", cli.addr)
	if err := cli.BaseService.OnStart(); err != nil {
		println("Error starting abci.grpcClient", "err", err)
		return err
	}

	// Start the main loop for processing responses.
	println("Starting main loop for processing responses")

	// This processes asynchronous request/response messages and dispatches
	// them to callbacks.
	go func() {
		println("Processing asynchronous request/response messages and dispatching them to callbacks")
		// Use a separate function to use defer for mutex unlocks (this handles panics)
		callCb := func(reqres *ReqRes) {
			cli.mtx.Lock()
			defer cli.mtx.Unlock()

			reqres.Done()

			// Notify client listener if set
			if cli.resCb != nil {
				cli.resCb(reqres.Request, reqres.Response)
			}

			// Notify reqRes listener if set
			reqres.InvokeCallback()
		}
		for reqres := range cli.chReqRes {
			if reqres != nil {
				callCb(reqres)
			} else {
				cli.Logger.Error("Received nil reqres")
			}
		}
	}()

RETRY_LOOP:
	for {
		conn, err := grpc.Dial(cli.addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			// grpc.WithContextDialer(dialerFunc),
		)
		if err != nil {
			println("Error dialing grpc", "err: %v", err)
			if cli.mustConnect {
				return err
			}
			cli.Logger.Error(fmt.Sprintf("abci.grpcClient failed to connect to %v.  Retrying...\n", cli.addr), "err", err)
			time.Sleep(time.Second * dialRetryIntervalSeconds)
			continue RETRY_LOOP
		}

		cli.Logger.Info("Dialed server. Waiting for echo.", "addr", cli.addr)
		client := consensus.NewConsensusApiClient(conn)
		cli.conn = conn

	ENSURE_CONNECTED:
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			valInfo, err := client.GetValidatorInfo(ctx, &emptypb.Empty{}, grpc.WaitForReady(true))
			if err == nil {
				println("Get validator info result: %v", valInfo)
				break ENSURE_CONNECTED
			} else {
				cli.Logger.Info("Get validator error.", "err", err)
			}
			echoRes, err := client.Echo(ctx, &consensus.RequestEcho{Message: "Hello Scalaris"}, grpc.WaitForReady(true))
			if err == nil {
				println("Echo result: %v", echoRes.Message)
				// go runSendTransaction(client)
				break ENSURE_CONNECTED
			}
			println("Echo error: %v", err.Error())
			cli.Logger.Error("Echo failed", "err", err)
			time.Sleep(time.Second * echoRetryIntervalSeconds)
		}

		cli.client = client
		return nil
	}
}

func (cli *grpcClient) OnStop() {
	cli.BaseService.OnStop()

	if cli.conn != nil {
		cli.conn.Close()
	}
	close(cli.chReqRes)
}

func (cli *grpcClient) StopForError(err error) {
	if !cli.IsRunning() {
		return
	}

	cli.mtx.Lock()
	if cli.err == nil {
		cli.err = err
	}
	cli.mtx.Unlock()

	cli.Logger.Error(fmt.Sprintf("Stopping abci.grpcClient for error: %v", err.Error()))
	if err := cli.Stop(); err != nil {
		cli.Logger.Error("Error stopping abci.grpcClient", "err", err)
	}
}

func (cli *grpcClient) Error() error {
	cli.mtx.Lock()
	defer cli.mtx.Unlock()
	return cli.err
}

// Set listener for all responses
// NOTE: callback may get internally generated flush responses.
func (cli *grpcClient) SetResponseCallback(resCb Callback) {
	cli.mtx.Lock()
	cli.resCb = resCb
	cli.mtx.Unlock()
}

// ----------------------------------------

func (cli *grpcClient) InitTransaction(ctx context.Context) (consensus.ConsensusApi_InitTransactionClient, error) {
	client, err := cli.client.InitTransaction(ctx)
	if err != nil {
		cli.StopForError(err)
		return nil, err
	}
	return client, nil
}

func (cli *grpcClient) Echo(ctx context.Context, req *consensus.RequestEcho) (*consensus.ResponseEcho, error) {
	return &consensus.ResponseEcho{Message: req.Message}, nil
}

func (cli *grpcClient) GetValidatorInfo(ctx context.Context) (*consensus.ValidatorInfo, error) {
	empty := &emptypb.Empty{}
	res, err := cli.client.GetValidatorInfo(ctx, empty)
	return res, err
}

func (cli *grpcClient) GetValidatorState(ctx context.Context) (*consensus.ValidatorInfo, error) {
	empty := &emptypb.Empty{}
	res, err := cli.client.GetValidatorInfo(ctx, empty)
	return res, err
}

func (cli *grpcClient) EchoAsync(msg string) *ReqRes {
	req := &consensus.Request{
		Value: &consensus.Request_Echo{&consensus.RequestEcho{Message: msg}},
	}
	res, err := cli.client.Echo(context.Background(), req.GetEcho(), grpc.WaitForReady(true))
	if err != nil {
		cli.StopForError(err)
	}
	return cli.finishAsyncCall(req, &consensus.Response{Value: &consensus.Response_Echo{Echo: res}})
}

// finishAsyncCall creates a ReqRes for an async call, and immediately populates it
// with the response. We don't complete it until it's been ordered via the channel.
func (cli *grpcClient) finishAsyncCall(req *consensus.Request, res *consensus.Response) *ReqRes {
	reqres := NewReqRes(req)
	reqres.Response = res
	cli.chReqRes <- reqres // use channel for async responses, since they must be ordered
	return reqres
}

// finishSyncCall waits for an async call to complete. It is necessary to call all
// sync calls asynchronously as well, to maintain call and response ordering via
// the channel, and this method will wait until the async call completes.
func (cli *grpcClient) finishSyncCall(reqres *ReqRes) *consensus.Response {
	// It's possible that the callback is called twice, since the callback can
	// be called immediately on SetCallback() in addition to after it has been
	// set. This is because completing the ReqRes happens in a separate critical
	// section from the one where the callback is called: there is a race where
	// SetCallback() is called between completing the ReqRes and dispatching the
	// callback.
	//
	// We also buffer the channel with 1 response, since SetCallback() will be
	// called synchronously if the reqres is already completed, in which case
	// it will block on sending to the channel since it hasn't gotten around to
	// receiving from it yet.
	//
	// ReqRes should really handle callback dispatch internally, to guarantee
	// that it's only called once and avoid the above race conditions.
	var once sync.Once
	ch := make(chan *consensus.Response, 1)
	reqres.SetCallback(func(res *consensus.Response) {
		once.Do(func() {
			ch <- res
		})
	})
	return <-ch
}

//----------------------------------------

func (cli *grpcClient) EchoSync(msg string) (*consensus.ResponseEcho, error) {
	reqres := cli.EchoAsync(msg)
	// StopForError should already have been called if error is set
	return cli.finishSyncCall(reqres).GetEcho(), cli.Error()
}
