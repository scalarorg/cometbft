package client

import (
	"fmt"

	cmtos "github.com/cometbft/cometbft/libs/os"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/types"
)

var (
	numValidators = 4
)

func MockGenesisValidators() ([]types.PrivValidator, error) {
	validators := make([]types.PrivValidator, numValidators)
	for i := 0; i < numValidators; i++ {
		var pv *privval.FilePV
		privValKeyFile := fmt.Sprintf("/mock/node%d/config/priv_validator_key.json", i)
		privValStateFile := fmt.Sprintf("/mock/node%d/data/priv_validator_state.json", i)

		if cmtos.FileExists(privValKeyFile) {
			pv = privval.LoadFilePV(privValKeyFile, privValStateFile)
		} else {
			pv = privval.GenFilePV(privValKeyFile, privValStateFile)
			pv.Save()
		}

		validators[i] = pv
	}

	return validators, nil
}
