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
		data, err := r.api.Recv()

		if err == io.EOF {
			// read done.
			r.Stop()
			return
		}

		if err != nil {
			r.Logger.Info("Listening transactions failed: ", err.Error())
			time.Sleep(2 * time.Second)

			continue
		}

		for i := range data.Blocks {
			block := data.Blocks[i]

			if len(block.Transactions) == 0 {
				continue
			}

			txs := make([]types.Tx, len(block.Transactions))

			r.Logger.Info("Received block", "num_txs", len(block.Transactions), "leader_round", data.LeaderRound)
			for i := range block.Transactions {
				txs[i] = block.Transactions[i]
			}

			_, err := r.consensusState.CreateScalarisBlock(&txs, r.client)

			if err != nil {
				r.Logger.Error("Error creating block", "err", err)
				continue
			}

			r.Logger.Info("Created block with transactions", "num_txs", len(block.Transactions))
		}
	}
}

func (r *Reactor) Stop() {
	r.Logger.Info("Stopping scalaris consensus reactor")
	r.api.CloseSend()
	r.client.Stop()
}

func (r *Reactor) SendExternalTx(tx []byte) {
	extTx := proto.ExternalTransaction{
		ChainId: "test-chain",
		TxBytes: [][]byte{
			[]byte(tx)},
	}
	err := r.api.Send(&extTx)
	if err != nil {
		r.Logger.Error("Failed to send transaction to scarlaris server with error", err)
	}
}
