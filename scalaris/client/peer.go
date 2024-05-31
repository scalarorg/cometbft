package client

import (
	"context"
	"io"
	"net"
	"time"

	cs "github.com/cometbft/cometbft/consensus"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/libs/service"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/p2p/conn"
	protomem "github.com/cometbft/cometbft/proto/tendermint/mempool"
	proto "github.com/cometbft/cometbft/scalaris/consensus/proto"
)

type Peer struct {
	*service.BaseService
	client               Client
	cpool                *cs.ConsensusPool
	consensusState       *cs.State
	api                  proto.ConsensusApi_InitTransactionClient
	Logger               log.Logger
	ip                   net.IP
	id                   p2p.ID
	addr                 *p2p.NetAddress
	kv                   map[string]interface{}
	Outbound, Persistent bool
}

// NewPeer creates and starts a new mock peer. If the ip
// is nil, random routable address is used.
func NewConsensusPeer(grpcServer string, cpool *cs.ConsensusPool, consensusState *cs.State, logger log.Logger) *Peer {
	client := NewGRPCClient(grpcServer, true)
	_, netAddr := p2p.CreateRoutableAddr()
	nodeKey := p2p.NodeKey{PrivKey: ed25519.GenPrivKey()}
	netAddr.ID = nodeKey.ID()
	cp := &Peer{
		client: client,
		cpool:  cpool,
		Logger: logger,
		ip:     nil,
		id:     nodeKey.ID(),
		addr:   netAddr,
		kv:     make(map[string]interface{}),
	}
	cp.BaseService = service.NewBaseService(nil, "GrpcPeer", cp)
	if err := cp.Start(); err != nil {
		panic(err)
	}
	return cp
}

func (cp *Peer) Start() error {
	err := cp.BaseService.Start()
	if err != nil {
		return err
	}
	cp.client.Start()
	client, err := cp.client.InitTransaction(context.Background())
	defer client.CloseSend()
	if err != nil {
		cp.Logger.Error("Error Init transaction", "err", err)
		return err
	}
	cp.api = client
	waitc := make(chan struct{})
	go func() {
		for {
			cp.Logger.Info("Waiting for commited transactions...")
			in, err := client.Recv()
			if err == io.EOF {
				// read done.
				close(waitc)
				return
			}
			if err != nil {
				cp.Logger.Info("client.Recv commited transactions failed: ", err.Error())
				cp.Logger.Info("Reconnecting to scalaris consensus client...")
				time.Sleep(2 * time.Second)

				continue
			}

			cp.Logger.Info("Received commited transactions", len(in.Transactions))
			txs := make(cs.Txs, len(in.Transactions))
			for _, tx := range in.Transactions {
				txs = append(txs, tx.TxBytes)
			}
			cp.consensusState.CreateScalarisProposalBlock(&txs)
			// cp.cpool.AddTxs(txs)
			// txs := in
			// if txs == nil || n.blockExec == nil || n.proxyApp == nil {
			// 	cp.logger.Info("Some consensus component is nil", txs, n.blockExec, n.proxyApp)
			// 	continue
			// }

			// cp.logger.Info("Got commited transactions: ", len(txs.Transactions))

			// _, blockHeight, err := n.blockExec.ApplyCommitedTransactions(cp.logger, n.proxyApp.Consensus(), in)
			// if err != nil {
			// 	cp.logger.Info("Commited block with error: ", err.Error())
			// 	continue
			// }
			// cp.logger.Info("New block height %s", blockHeight)
		}
	}()
	<-waitc

	cp.Logger.Info("Started scalar consensus client")
	return nil
}
func (cp *Peer) Stop() error {
	err := cp.api.CloseSend()
	if err != nil {
		cp.Logger.Error("Close consensus client with error", err)
	}
	return cp.BaseService.Stop()
}
func (cp *Peer) FlushStop() { cp.Stop() } //nolint:errcheck //ignore error
func (cp *Peer) TrySendEnvelope(e p2p.Envelope) bool {
	return cp.SendEnvelope(e)
}
func (cp *Peer) SendEnvelope(e p2p.Envelope) bool {
	switch msg := e.Message.(type) {
	case *protomem.Txs:
		protoTxs := msg.GetTxs()
		if len(protoTxs) == 0 {
			cp.Logger.Error("received empty txs from peer", "src", e.Src)
			return true
		}
		var err error
		for _, tx := range protoTxs {
			extTx := proto.ExternalTransaction{
				Namespace: "AbciAdapter",
				TxBytes:   tx,
			}
			err = cp.api.Send(&extTx)
			if err != nil {
				cp.Logger.Error("Send transaction with error", err)
				return false
			}
		}
		return true
	default:
		cp.Logger.Error("unknown message type", "src", e.Src, "chId", e.ChannelID, "msg", e.Message)
		return false
	}
}
func (cp *Peer) TrySend(_ byte, _ []byte) bool { return true }
func (cp *Peer) Send(_ byte, _ []byte) bool    { return true }
func (cp *Peer) NodeInfo() p2p.NodeInfo {
	return p2p.DefaultNodeInfo{
		DefaultNodeID: cp.addr.ID,
		ListenAddr:    cp.addr.DialString(),
	}
}
func (cp *Peer) Status() conn.ConnectionStatus { return conn.ConnectionStatus{} }
func (cp *Peer) ID() p2p.ID                    { return cp.id }
func (cp *Peer) IsOutbound() bool              { return cp.Outbound }
func (cp *Peer) IsPersistent() bool            { return cp.Persistent }
func (cp *Peer) Get(key string) interface{} {
	if value, ok := cp.kv[key]; ok {
		return value
	}
	return nil
}
func (cp *Peer) Set(key string, value interface{}) {
	cp.kv[key] = value
}
func (cp *Peer) RemoteIP() net.IP            { return cp.ip }
func (cp *Peer) SocketAddr() *p2p.NetAddress { return cp.addr }
func (cp *Peer) RemoteAddr() net.Addr        { return &net.TCPAddr{IP: cp.ip, Port: 8800} }
func (cp *Peer) CloseConn() error            { return nil }
func (cp *Peer) SetRemovalFailed()           {}
func (cp *Peer) GetRemovalFailed() bool      { return false }
