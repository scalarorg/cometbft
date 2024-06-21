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
	totalTxsSent   int
	pool           ConsensusMempool
	newTxNotify    chan struct{}
}

func NewReactor(client Client, consensusState *cs.State) *Reactor {
	return &Reactor{
		client:         client,
		consensusState: consensusState,
		totalTxsSent:   0,
		pool:           NewConsensusMempool(),
		newTxNotify:    make(chan struct{}),
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

	// Start the transaction receiving routine
	go r.receiveTxRoutine()

	// Start the transaction processing routine
	go r.processTxsRoutine()

	return nil
}

func (r *Reactor) receiveTxRoutine() {
	num_received_txs := 0

	for {
		data, err := r.api.Recv()

		if err == io.EOF {
			// read done.
			r.Logger.Error("Connection closed by server", "err", err)
			r.Stop()
			return
		}

		if err != nil {
			r.Logger.Error("Listening transactions failed: ", err)

			// Retry
			err = r.client.Connect()

			if err != nil {
				r.Logger.Error("Failed to reconnect to server", "err", err)
			}

			time.Sleep(2 * time.Second)

			continue
		}

		for i := range data.Blocks {
			block := data.Blocks[i]

			if len(block.Transactions) == 0 {
				continue
			}

			num_received_txs += len(block.Transactions)

			r.Logger.Info("Received block", "num_txs", len(block.Transactions), "num_received_txs", num_received_txs)
			for i := range block.Transactions {
				r.pool.AddTx(block.Transactions[i])
			}
			go func() {
				r.newTxNotify <- struct{}{}
			}()
		}
	}
}

func (r *Reactor) processTxsRoutine() {
	num_processed_txs := 0
	num_invalid_txs := 0

	for {
		txs := r.pool.PopTxs(1000)

		if len(txs) == 0 {
			r.Logger.Info("No transactions to process. Waiting for new transactions")
			<-r.newTxNotify
			continue
		}

		r.Logger.Info("Processing transactions", "num_txs", len(txs))
		_, err := r.consensusState.CreateScalarisBlock(&txs, r.client)

		if err != nil {
			num_invalid_txs += len(txs)
			r.Logger.Error("Error creating block", "num_invalid_txs", num_invalid_txs, "err", err)
			continue
		}

		num_processed_txs += len(txs)
		r.Logger.Info("Created block with transactions", "num_txs", len(txs), "num_processed_txs", num_processed_txs)
	}
}

func (r *Reactor) Stop() {
	r.Logger.Info("Stopping scalaris consensus reactor")
	r.api.CloseSend()
	r.client.Stop()
}

func (r *Reactor) SendExternalTx(tx []byte) {
	r.totalTxsSent++
	extTx := proto.ExternalTransaction{
		ChainId: "test-chain",
		TxBytes: [][]byte{
			[]byte(tx)},
	}
	r.Logger.Info("Sending transaction to scalaris server", "totalTxsSent", r.totalTxsSent)
	err := r.api.Send(&extTx)
	if err != nil {
		r.Logger.Error("Failed to send transaction to scarlaris server with error", err)
	}
}
