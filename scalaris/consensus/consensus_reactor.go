package consensus

import (
	"context"
	"io"
	"time"

	cs "github.com/cometbft/cometbft/consensus"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/scalaris/client"
	proto "github.com/cometbft/cometbft/scalaris/consensus/proto"
	"github.com/cometbft/cometbft/types"
)

type Client client.Client

type Reactor struct {
	Logger         log.Logger
	client         Client
	api            proto.ConsensusApi_InitTransactionClient
	consensusState *cs.State
}

func NewReactor(client Client, consensusState *cs.State) *Reactor {
	return &Reactor{
		client:         client,
		consensusState: consensusState,
	}
}

func (r *Reactor) SetLogger(logger log.Logger) {
	// Set the logger for the client
	r.Logger = logger
}

func (r *Reactor) Start() error {
	api, err := r.client.InitTransaction(context.Background())

	if err != nil {
		return err
	}

	r.api = api

	if r.consensusState.Height == 1 {
		r.Logger.Info("No blocks committed yet. Initializing state with genesis block")
		// Initialize the state with empty block
		txs := make([]types.Tx, 0)

		_, err := r.consensusState.CreateScalarisBlock(&txs, r.client)
		if err != nil {
			return err
		}
	}

	// Start the reactor in a goroutine
	go r.receiveTxRoutine()

	return nil
}

func (r *Reactor) receiveTxRoutine() {
	for {
		r.Logger.Info("Waiting for commited transactions...")
		data, err := r.api.Recv()

		if err == io.EOF {
			// read done.
			r.Stop()
			return
		}

		if err != nil {
			r.Logger.Info("client.Recv commited transactions failed: ", err.Error())
			time.Sleep(2 * time.Second)

			continue
		}

		txs := make([]types.Tx, len(data.Transactions))

		r.Logger.Info("Received commited transactions", "num_txs", len(txs))
		for i, tx := range data.Transactions {
			txs[i] = tx.TxBytes
		}

		block, err := r.consensusState.CreateScalarisBlock(&txs, r.client)

		if err != nil {
			r.Logger.Error("Error creating block", "err", err)
			continue
		}

		r.Logger.Info("Created block with transactions", "num_txs", len(block.Txs))
	}
}

func (r *Reactor) Stop() {
	r.Logger.Info("Stopping scalaris consensus reactor")
	r.api.CloseSend()
	r.client.Stop()
}

func (r *Reactor) SendExternalTx(tx []byte) {
	extTx := proto.ExternalTransaction{
		Namespace: "AbciAdapter",
		TxBytes:   tx,
	}
	r.Logger.Info("Send transaction to scalaris consensus server...")
	err := r.api.Send(&extTx)
	if err != nil {
		r.Logger.Error("Failed to send transaction to scarlaris server with error", err)
	}
}
