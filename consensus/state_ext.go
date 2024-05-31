package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/cometbft/cometbft/types"
)

/*
//-----------------------------------------
// the main go routines in consensus state

// receiveRoutine handles messages which may cause state transitions.
// it's argument (n) is the number of messages to process before exiting - use 0 to run forever
// It keeps the RoundState and is the only thing that updates it.
// Updates (state transitions) happen on timeouts, complete proposals, and 2/3 majorities.
// State must be locked before any internal state is updated.
func (cs *State) receiveRoutine(maxSteps int) {
}
*/
type FnDecideProposal func(height int64, round int32)
type FnDoPrevote func(height int64, round int32)
type FnSetProposal func(proposal *types.Proposal) error
type Txs []types.Tx

type ConsensusPool struct {
	mu       sync.Mutex
	blockTxs []Txs
}

func NewConsensusPool() *ConsensusPool {
	return &ConsensusPool{
		blockTxs: make([]Txs, 0),
	}
}

func (pool *ConsensusPool) GetTxs() *Txs {
	txl := Txs{}
	return &txl
}
func (pool *ConsensusPool) AddTxs(txs Txs) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.blockTxs = append(pool.blockTxs, txs)
}

// Get returns the item at the specified index
func (pool *ConsensusPool) Get(index int) (interface{}, bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if index < 0 || index >= len(pool.blockTxs) {
		return nil, false
	}
	return pool.blockTxs[index], true
}
func (pool *ConsensusPool) RemoveFirst() *Txs {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if len(pool.blockTxs) == 0 {
		return nil
	}
	first := pool.blockTxs[0]
	pool.blockTxs = pool.blockTxs[1:]
	return &first
}
func (pool *ConsensusPool) Remove(index int) bool {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if index < 0 || index >= len(pool.blockTxs) {
		return false
	}
	pool.blockTxs = append(pool.blockTxs[:index], pool.blockTxs[index+1:]...)
	return true
}

// Len returns the length of the list
func (pool *ConsensusPool) Len() int {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return len(pool.blockTxs)
}

// Create global variable for keep commited transaction received from consensus client
var (
	// set by Node
	consensusPool *ConsensusPool
)

// SetEnvironment sets up the given Environment.
// It will race if multiple Node call SetEnvironment.
func SetConsensusPool(pool *ConsensusPool) {
	consensusPool = pool
}

func (cs *State) ExtendConsensusState() {
	cs.decideProposal = cs.scalarisDecideProposal
}
func (cs *State) SetFnDoPrevote(doPrevote FnDoPrevote) {
	cs.doPrevote = doPrevote
}

func (cs *State) SetFnSetProposal(setProposal FnSetProposal) {
	cs.setProposal = setProposal
}
func (cs *State) CreateScalarisProposalBlock(txs *Txs) (*types.Block, error) {
	if cs.privValidator == nil {
		return nil, errors.New("entered createProposalBlock with privValidator being nil")
	}

	var commit *types.Commit
	switch {
	case cs.Height == cs.state.InitialHeight:
		// We're creating a proposal for the first block.
		// The commit is empty, but not nil.
		commit = types.NewCommit(0, 0, types.BlockID{}, nil)

	case cs.LastCommit.HasTwoThirdsMajority():
		// Make the commit from LastCommit
		commit = cs.LastCommit.MakeCommit()

	default: // This shouldn't happen.
		return nil, errors.New("propose step; cannot propose anything without commit for the previous block")
	}

	if cs.privValidatorPubKey == nil {
		// If this node is a validator & proposer in the current round, it will
		// miss the opportunity to create a block.
		return nil, fmt.Errorf("propose step; empty priv validator public key, error: %w", errPubKeyIsNotSet)
	}

	proposerAddr := cs.privValidatorPubKey.Address()
	/*
	 * Call blockExec.CreateProposalBlock
	 * In this method, a RequestPrepareProposal call is made to application layer to filter out proposal txs,
	 * Only returned txs are included into proposal block
	 */
	// ret, err := cs.blockExec.CreateProposalBlock(cs.Height, cs.state, commit, proposerAddr)
	// if err != nil {
	// 	panic(err)
	// }
	//Directly create block
	evidence := []types.Evidence{}
	ret := cs.state.MakeBlock(cs.Height, *txs, commit, evidence, proposerAddr)
	cs.finalizeCommit(cs.Height)
	return ret, nil
}

func (cs *State) scalarisDecideProposal(height int64, round int32) {
	var block *types.Block
	var blockParts *types.PartSet

	// Decide on block
	if cs.ValidBlock != nil {
		// If there is valid block, choose that.
		block, blockParts = cs.ValidBlock, cs.ValidBlockParts
	} else {
		// Create a new proposal block from state/txs from the mempool.
		var err error
		txs := consensusPool.GetTxs()
		block, err = cs.CreateScalarisProposalBlock(txs)
		if err != nil {
			cs.Logger.Error("unable to create proposal block", "error", err)
			return
		} else if block == nil {
			panic("Method createProposalBlock should not provide a nil block without errors")
		}
		cs.metrics.ProposalCreateCount.Add(1)
		blockParts, err = block.MakePartSet(types.BlockPartSizeBytes)
		if err != nil {
			cs.Logger.Error("unable to create proposal block part set", "error", err)
			return
		}
	}

	// Flush the WAL. Otherwise, we may not recompute the same proposal to sign,
	// and the privValidator will refuse to sign anything.
	if err := cs.wal.FlushAndSync(); err != nil {
		cs.Logger.Error("failed flushing WAL to disk")
	}

	// Make proposal
	propBlockID := types.BlockID{Hash: block.Hash(), PartSetHeader: blockParts.Header()}
	proposal := types.NewProposal(height, round, cs.ValidRound, propBlockID)
	p := proposal.ToProto()
	if err := cs.privValidator.SignProposal(cs.state.ChainID, p); err == nil {
		proposal.Signature = p.Signature

		// // send proposal and block parts on internal msg queue
		// cs.sendInternalMessage(msgInfo{&ProposalMessage{proposal}, ""})

		// for i := 0; i < int(blockParts.Total()); i++ {
		// 	part := blockParts.GetPart(i)
		// 	cs.sendInternalMessage(msgInfo{&BlockPartMessage{cs.Height, cs.Round, part}, ""})
		// }

		cs.Logger.Debug("signed proposal", "height", height, "round", round, "proposal", proposal)
	} else if !cs.replayMode {
		cs.Logger.Error("propose step; failed signing proposal", "height", height, "round", round, "err", err)
	}
}
