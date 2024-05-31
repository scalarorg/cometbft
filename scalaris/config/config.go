package config

import (
	cmtcfg "github.com/cometbft/cometbft/config"
)

// Config defines the top level configuration for a CometBFT node
type Config struct {
	// Top level options use an anonymous struct
	cmtcfg.BaseConfig `mapstructure:",squash"`

	// Options for services
	RPC             *cmtcfg.RPCConfig             `mapstructure:"rpc"`
	P2P             *cmtcfg.P2PConfig             `mapstructure:"p2p"`
	Mempool         *cmtcfg.MempoolConfig         `mapstructure:"mempool"`
	StateSync       *cmtcfg.StateSyncConfig       `mapstructure:"statesync"`
	BlockSync       *cmtcfg.BlockSyncConfig       `mapstructure:"blocksync"`
	Consensus       *cmtcfg.ConsensusConfig       `mapstructure:"consensus"`
	Storage         *cmtcfg.StorageConfig         `mapstructure:"storage"`
	TxIndex         *cmtcfg.TxIndexConfig         `mapstructure:"tx_index"`
	Instrumentation *cmtcfg.InstrumentationConfig `mapstructure:"instrumentation"`
	ScalarisAddr    string                        `mapstructure:"scalaris_addr"`
}

// DefaultConfig returns a default configuration for a CometBFT node.
func DefaultConfig() *Config {
	return &Config{
		BaseConfig:      cmtcfg.DefaultBaseConfig(),
		RPC:             cmtcfg.DefaultRPCConfig(),
		P2P:             cmtcfg.DefaultP2PConfig(),
		Mempool:         cmtcfg.DefaultMempoolConfig(),
		StateSync:       cmtcfg.DefaultStateSyncConfig(),
		BlockSync:       cmtcfg.DefaultBlockSyncConfig(),
		Consensus:       cmtcfg.DefaultConsensusConfig(),
		Storage:         cmtcfg.DefaultStorageConfig(),
		TxIndex:         cmtcfg.DefaultTxIndexConfig(),
		Instrumentation: cmtcfg.DefaultInstrumentationConfig(),
		ScalarisAddr:    "127.0.0.1:8080",
	}
}

// SetRoot sets the RootDir for all Config structs
func (cfg *Config) SetRoot(root string) *Config {
	cfg.BaseConfig.RootDir = root
	cfg.RPC.RootDir = root
	cfg.P2P.RootDir = root
	cfg.Mempool.RootDir = root
	cfg.Consensus.RootDir = root
	return cfg
}

func (cfg *Config) CheckDeprecated() []string {
	var warnings []string
	if cfg.Mempool.Version == cmtcfg.MempoolV1 {
		warnings = append(warnings, "prioritized mempool detected. This version of the mempool will be removed in the next major release.")
	}
	if cfg.Mempool.TTLNumBlocks != 0 {
		warnings = append(warnings, "prioritized mempool key detected. This key, together with this version of the mempool, will be removed in the next major release.")
	}
	if cfg.Mempool.TTLDuration != 0 {
		warnings = append(warnings, "prioritized mempool key detected. This key, together with this version of the mempool, will be removed in the next major release.")
	}
	if !cfg.BaseConfig.BlockSyncMode {
		warnings = append(warnings, "disabled block_sync key detected. BlockSync will be enabled unconditionally in the next major release and this key will be removed.")
	}
	if cfg.BaseConfig.DeprecatedFastSyncMode != nil {
		warnings = append(warnings, "fast_sync key detected. This key has been renamed to block_sync. The value of this deprecated key will be disregarded.")
	}
	if cfg.P2P.UPNP != nil {
		warnings = append(warnings, "unused and deprecated upnp field detected in P2P config.")
	}
	return warnings
}
