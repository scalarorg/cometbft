package state

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/types"
)

// CreateProposalBlock calls state.MakeBlock with evidence from the evpool
// and txs from the mempool. The max bytes must be big enough to fit the commit.
// Up to 1/10th of the block space is allocated for maximum sized evidence.
// The rest is given to txs, up to the max gas.
//
// Contract: application will not return more bytes than are sent over the wire.
func (blockExec *BlockExecutor) CreateScalarisProposalBlock(
	height int64,
	state State,
	lastExtCommit *types.ExtendedCommit,
	proposerAddr []byte,
	txs *([]types.Tx),
) (*types.Block, error) {

	maxBytes := state.ConsensusParams.Block.MaxBytes
	// maxGas := state.ConsensusParams.Block.MaxGas

	evidence, evSize := blockExec.evpool.PendingEvidence(state.ConsensusParams.Evidence.MaxBytes)

	// Fetch a limited amount of valid txs
	maxDataBytes := types.MaxDataBytes(maxBytes, evSize, state.Validators.Size())

	// TODO-SCALARIS: Try to get the wanted gas from each transaction while checking the maxGas
	// txs := blockExec.mempool.ReapMaxBytesMaxGas(maxDataBytes, maxGas)

	// Use the txs passed in without checking the maxGas, maxDataBytes
	commit := lastExtCommit.ToCommit()
	block := state.MakeBlock(height, *txs, commit, evidence, proposerAddr)

	// Call `PrepareProposal` to ABCI application to get the transactions to include in the block
	// TODO-SCALARIS: Only call `PrepareProposal` from the proposer node
	rpp, err := blockExec.proxyApp.PrepareProposal(
		context.TODO(),
		&abci.RequestPrepareProposal{
			MaxTxBytes:         maxDataBytes,
			Txs:                block.Txs.ToSliceOfBytes(),
			LocalLastCommit:    buildExtendedCommitInfoFromStore(lastExtCommit, blockExec.store, state.InitialHeight, state.ConsensusParams.ABCI),
			Misbehavior:        block.Evidence.Evidence.ToABCI(),
			Height:             block.Height,
			Time:               block.Time,
			NextValidatorsHash: block.NextValidatorsHash,
			ProposerAddress:    block.ProposerAddress,
		},
	)

	if err != nil {
		// The App MUST ensure that only valid (and hence 'processable') transactions
		// enter the mempool. Hence, at this point, we can't have any non-processable
		// transaction causing an error.
		//
		// Also, the App can simply skip any transaction that could cause any kind of trouble.
		// Either way, we cannot recover in a meaningful way, unless we skip proposing
		// this block, repair what caused the error and try again. Hence, we return an
		// error for now (the production code calling this function is expected to panic).
		return nil, err
	}

	txl := types.ToTxs(rpp.Txs)
	if err := txl.Validate(maxDataBytes); err != nil {
		return nil, err
	}

	return state.MakeBlock(height, txl, commit, evidence, proposerAddr), nil
}
