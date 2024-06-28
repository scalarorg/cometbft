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
	Logger           log.Logger
	client           Client
	api              proto.ConsensusApi_InitTransactionClient
	consensusState   *cs.State
	consensusReactor *cs.Reactor
	totalTxsSent     int
	pool             ConsensusMempool
	newTxNotify      chan struct{}
}

func NewReactor(client Client, consensusState *cs.State, consensusReactor *cs.Reactor) *Reactor {
	return &Reactor{
		client:           client,
		consensusState:   consensusState,
		consensusReactor: consensusReactor,
		totalTxsSent:     0,
		pool:             NewConsensusMempool(),
		newTxNotify:      make(chan struct{}),
	}
}

func (r *Reactor) SetLogger(logger log.Logger) {
	// Set the logger for the client
	r.Logger = logger
}

func (r *Reactor) Start() error {
	err := r.StartConsensusApiClient()

	if err != nil {
		return err
	}

	go r.startConsensusRoutine()

	return nil
}

func (r *Reactor) startConsensusRoutine() {
	for !r.consensusReactor.WaitSync() {
		r.Logger.Info("Waiting for consensus reactor to sync")
		time.Sleep(2 * time.Second)
	}


	if r.consensusState.Height == 1 {
		r.Logger.Debug("No blocks committed yet. Initializing state with genesis block")
		// Initialize the state with empty block
		txs := make([]types.Tx, 0)

		_, err := r.consensusState.CreateScalarisBlock(&txs, r.client)
		if err != nil {
			r.Logger.Error("Error while creating genesis block", "err", err)
			return
		}
	}

	r.Logger.Debug("State/Block synced. Starting Scalaris consensus routine")

	// Start the transaction receiving routine
	go r.receiveTxRoutine()

	// Start the transaction processing routine
	go r.processTxsRoutine()
}

func (r *Reactor) StartConsensusApiClient() error {
	api, err := r.client.InitTransaction(context.Background())

	if err != nil {
		return err
	}

	r.api = api

	return nil
}

func (r *Reactor) receiveTxRoutine() {
	numReceivedTxs := 0

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
				time.Sleep(2 * time.Second)
				continue
			}

			err = r.StartConsensusApiClient()

			if err != nil {
				r.Logger.Error("Failed to start consensus api client", "err", err)
				time.Sleep(2 * time.Second)
				continue
			}

			continue
		}

		for i := range data.Blocks {
			block := data.Blocks[i]

			if len(block.Transactions) == 0 {
				continue
			}

			numReceivedTxs += len(block.Transactions)

			r.Logger.Debug("Received block", "num_txs", len(block.Transactions), "num_received_txs", numReceivedTxs)
			for i := range block.Transactions {
				r.pool.AddTx(block.Transactions[i])
			}

			// Notify the transaction processing routine
			go func() {
				r.newTxNotify <- struct{}{}
			}()
		}
	}
}

func (r *Reactor) processTxsRoutine() {
	// TODO_SCALARIS: Change the max block txs to a more reasonable value / make it configurable
	const MAX_BLOCK_TXS = 1000

	numProcessedTxs := 0
	numInvalidTxs := 0

	for {
		txs := r.pool.PopTxs(MAX_BLOCK_TXS)

		if len(txs) == 0 {
			// Wait for new transactions to be added to the pool
			<-r.newTxNotify
			continue
		}

		r.Logger.Debug("Processing transactions", "num_txs", len(txs))
		_, err := r.consensusState.CreateScalarisBlock(&txs, r.client)

		if err != nil {
			numInvalidTxs += len(txs)
			r.Logger.Error("Error while creating block", "err", err, "num_invalid_txs", numInvalidTxs)
			continue
		}

		numProcessedTxs += len(txs)
		r.Logger.Debug("Created block", "num_txs", len(txs), "num_processed_txs", numProcessedTxs)
	}
}

func (r *Reactor) Stop() {
	r.Logger.Info("Stopping Scalaris consensus reactor")
	r.api.CloseSend()
	r.client.Stop()
}

func (r *Reactor) SendExternalTx(tx []byte) {
	r.totalTxsSent++
	extTx := proto.ExternalTransaction{
		// TODO_SCALARIS: Change the chain id to the actual chain id
		ChainId: "test-chain",
		TxBytes: [][]byte{
			[]byte(tx)},
	}
	r.Logger.Info("Sending transaction to Scalaris server", "totalTxsSent", r.totalTxsSent)
	err := r.api.Send(&extTx)
	if err != nil {
		r.Logger.Error("Failed to send transaction to Scarlaris server with error", err)
	}
}
