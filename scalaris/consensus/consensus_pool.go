package consensus

import (
	"container/list"

	"github.com/cometbft/cometbft/types"
)

type ConsensusMempool struct {
	txQueue *list.List
}

func NewConsensusMempool() ConsensusMempool {
	return ConsensusMempool{
		txQueue: list.New(),
	}
}

func (m *ConsensusMempool) AddTx(tx types.Tx) {
	m.txQueue.PushBack(tx)
}

func (m *ConsensusMempool) PopTxs(numTx int) []types.Tx {
	var txs []types.Tx

	for i := 0; i < numTx; i++ {
		if m.txQueue.Len() == 0 {
			break
		}
		tx := m.txQueue.Front()
		switch tx := tx.Value.(type) {
		case types.Tx:
			txs = append(txs, tx)
		}
		m.txQueue.Remove(tx)
	}
	return txs
}
