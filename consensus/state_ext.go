package consensus

import (
	"errors"
	"fmt"
	"sync"

	cstypes "github.com/cometbft/cometbft/consensus/types"
	"github.com/cometbft/cometbft/p2p"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/scalaris/client"
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

func (cs *State) SetFnDoPrevote(doPrevote FnDoPrevote) {
	cs.doPrevote = doPrevote
}

func (cs *State) SetFnSetProposal(setProposal FnSetProposal) {
	cs.setProposal = setProposal
}

// Create the next block from Scalaris consensus's returned transactions
//
// Set the votes, precommits same as CometBFT consensus does, without depend on CometBFT's P2Ps
func (cs *State) CreateScalarisBlock(txs *[]types.Tx, client client.Client) (*types.Block, error) {
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

	// Temporary: Make the first validator is the proposer
	// TODO-SCALARIS: Find a better way to decide which validator is proposer one
	proposer := cs.Validators.Validators[0]
	proposerAddr := proposer.PubKey.Address()

	// Call `PrepareProposal` to ABCI application to get the transactions to include in the block
	// TODO-SCALARIS: Only call `PrepareProposal` from the proposer node
	ret, err := cs.blockExec.CreateScalarisProposalBlock(cs.Height, cs.state, commit, proposerAddr, txs)

	if err != nil {
		cs.Logger.Error("unable to create proposal block", "error", err)
		return nil, err
	}

	blockParts, err := ret.MakePartSet(types.BlockPartSizeBytes)
	if err != nil {
		cs.Logger.Error("unable to create proposal block part set", "error", err)
		return nil, err
	}

	// Make proposal
	propBlockID := types.BlockID{Hash: ret.Hash(), PartSetHeader: blockParts.Header()}
	proposal := types.NewProposal(ret.Height, cs.Round, cs.ValidRound, propBlockID)

	p := proposal.ToProto()

	// Sign the proposal
	client.SignProposal(cs.state.ChainID, p)
	proposal.Signature = p.Signature

	// Set the proposal to the state
	cs.setProposal(proposal)
	cs.ProposalBlockParts = blockParts
	cs.ProposalBlock = ret

	// Move to Prevote step
	cs.updateRoundStep(cs.Round, cstypes.RoundStepPropose)
	cs.newStep()

	// Call `ProcessProposal` to ABCI application to validate the proposed block
	isAppValid, err := cs.blockExec.ProcessProposal(cs.ProposalBlock, cs.state)
	if err != nil {
		panic(fmt.Sprintf(
			"state machine returned an error (%v) when calling ProcessProposal", err,
		))
	}

	if !isAppValid {
		cs.Logger.Error("prevote step: state machine rejected a proposed block; this should not happen:"+
			"the proposer may be misbehaving; prevoting nil", "err", err)
		return nil, nil
	}

	privValidators, err := cs.getPrivValidators(client)

	if err != nil {
		return nil, err
	}

	// If len(privValidators) < 2/3 of validators, return nil
	if len(privValidators) <= 2*len(cs.Validators.Validators)/3 {
		cs.Logger.Error("prevote step: less than 2/3 of validators are available; prevoting nil")
		return nil, nil
	}

	// Sign prevote votes
	votes, err := cs.signVotes(cmtproto.PrevoteType, cs.ProposalBlock.Hash(), cs.ProposalBlockParts.Header(), &privValidators)
	if err != nil {
		return nil, err
	}

	// Add the votes to the state
	cs.tryAddVotes(votes)

	// Update the valid values
	cs.ValidBlock = ret
	cs.ValidBlockParts = blockParts
	cs.ValidRound = cs.Round

	// Move to Precommit step
	cs.updateRoundStep(cs.Round, cstypes.RoundStepPrevote)
	cs.newStep()

	if err := cs.eventBus.PublishEventLock(cs.RoundStateEvent()); err != nil {
		cs.Logger.Error("failed publishing event lock", "err", err)
	}

	// Sign precommit votes
	votes, err = cs.signVotes(cmtproto.PrecommitType, cs.ProposalBlock.Hash(), cs.ProposalBlockParts.Header(), &privValidators)
	if err != nil {
		return nil, err
	}

	// Add the votes to the state
	// Can make transition to the next round
	cs.tryAddVotes(votes)

	// Update the locked values
	cs.LockedBlock = cs.ProposalBlock
	cs.LockedBlockParts = cs.ProposalBlockParts
	cs.LockedRound = cs.Round

	// Move to Commit step
	cs.updateRoundStep(cs.Round, cstypes.RoundStepPrecommit)
	cs.newStep()

	cs.enterCommit(cs.Height, cs.Round)

	return ret, nil
}

func (cs *State) tryAddVotes(votes []types.Vote) {
	for idx := range votes {
		cs.Votes.AddVote(&votes[idx], p2p.ID(votes[idx].ValidatorAddress.String()))
	}
}

func (cs *State) getPrivValidators(client client.Client) ([]types.PrivValidator, error) {
	privValidators, err := client.GetPrivValidators()
	if err != nil {
		cs.Logger.Error("unable to get priv validators", "error", err)
		return nil, err
	}

	// Filter out the validators who are not in the cs.Validators.Validators list
	validators := make([]types.PrivValidator, 0)

	for _, v := range cs.Validators.Validators {
		for _, val := range privValidators {
			pubKey, err := val.GetPubKey()
			if err != nil {
				cs.Logger.Error("unable to get public key from priv validator", "error", err)
				return nil, err
			}
			if pubKey.Equals(v.PubKey) {
				validators = append(validators, val)
				break
			}
		}
	}
	return validators, nil
}

func (cs *State) signVotes(msgType cmtproto.SignedMsgType,
	hash []byte,
	header types.PartSetHeader, validators *[]types.PrivValidator) ([]types.Vote, error) {
	votes := make([]types.Vote, len(*validators))

	for i, val := range *validators {
		pubKey, err := val.GetPubKey()
		if err != nil {
			cs.Logger.Error("unable to get public key from priv validator", "error", err)
			return nil, err
		}
		addr := pubKey.Address()

		valIdx, _ := cs.Validators.GetByAddress(addr)
		vote := types.Vote{
			ValidatorAddress: addr,
			ValidatorIndex:   valIdx,
			Height:           cs.Height,
			Round:            cs.Round,
			Timestamp:        cs.voteTime(),
			Type:             msgType,
			BlockID: types.BlockID{
				Hash:          hash,
				PartSetHeader: header,
			},
		}

		v := vote.ToProto()

		// Sign the vote
		err = val.SignVote(cs.state.ChainID, v)
		if err != nil {
			cs.Logger.Error("unable to sign vote", "error", err)
			return nil, err
		}
		vote.Signature = v.Signature
		vote.Timestamp = v.Timestamp

		votes[i] = vote
	}

	return votes, nil
}
