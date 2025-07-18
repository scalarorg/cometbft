package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"

	_ "net/http/pprof" //nolint: gosec

	abcicli "github.com/cometbft/cometbft/v2/abci/client"
	abcitypes "github.com/cometbft/cometbft/v2/abci/types"
	cfg "github.com/cometbft/cometbft/v2/config"
	bc "github.com/cometbft/cometbft/v2/internal/blocksync"
	cs "github.com/cometbft/cometbft/v2/internal/consensus"
	"github.com/cometbft/cometbft/v2/internal/evidence"
	cmtjson "github.com/cometbft/cometbft/v2/libs/json"
	"github.com/cometbft/cometbft/v2/libs/log"
	cmtpubsub "github.com/cometbft/cometbft/v2/libs/pubsub"
	"github.com/cometbft/cometbft/v2/libs/service"
	"github.com/cometbft/cometbft/v2/light"
	mempl "github.com/cometbft/cometbft/v2/mempool"
	"github.com/cometbft/cometbft/v2/p2p"
	na "github.com/cometbft/cometbft/v2/p2p/netaddr"
	"github.com/cometbft/cometbft/v2/p2p/pex"
	"github.com/cometbft/cometbft/v2/p2p/transport/tcp"
	"github.com/cometbft/cometbft/v2/proxy"
	rpccore "github.com/cometbft/cometbft/v2/rpc/core"
	grpcserver "github.com/cometbft/cometbft/v2/rpc/grpc/server"
	grpcprivserver "github.com/cometbft/cometbft/v2/rpc/grpc/server/privileged"
	rpcserver "github.com/cometbft/cometbft/v2/rpc/jsonrpc/server"
	sm "github.com/cometbft/cometbft/v2/state"
	"github.com/cometbft/cometbft/v2/state/indexer"
	"github.com/cometbft/cometbft/v2/state/txindex"
	"github.com/cometbft/cometbft/v2/state/txindex/null"
	"github.com/cometbft/cometbft/v2/statesync"
	"github.com/cometbft/cometbft/v2/store"
	"github.com/cometbft/cometbft/v2/types"
	cmttime "github.com/cometbft/cometbft/v2/types/time"
	"github.com/cometbft/cometbft/v2/version"
)

// Node is the highest level interface to a full CometBFT node.
// It includes all configuration information and running services.
type Node struct {
	service.BaseService

	// config
	config        *cfg.Config
	genesisTime   time.Time
	privValidator types.PrivValidator // local node's validator key

	// network
	transport   *tcp.MultiplexTransport
	sw          *p2p.Switch  // p2p connections
	addrBook    pex.AddrBook // known peers
	nodeInfo    p2p.NodeInfo
	nodeKey     *p2p.NodeKey // our node privkey
	isListening bool

	// services
	eventBus         *types.EventBus // pub/sub for services
	stateStore       sm.Store
	blockStore       *store.BlockStore // store the blockchain to disk
	pruner           *sm.Pruner
	bcReactor        p2p.Reactor    // for block-syncing
	mempoolReactor   mempoolReactor // for gossipping transactions
	mempool          mempl.Mempool
	consensusState   *cs.State      // latest consensus state
	consensusReactor *cs.Reactor    // for participating in the consensus
	pexReactor       *pex.Reactor   // for exchanging peer addresses
	evidencePool     *evidence.Pool // tracking evidence
	proxyApp         proxy.AppConns // connection to the application
	rpcListeners     []net.Listener // rpc servers
	txIndexer        txindex.TxIndexer
	blockIndexer     indexer.BlockIndexer
	indexerService   *txindex.IndexerService
	prometheusSrv    *http.Server
	pprofSrv         *http.Server

	// statesync
	stateSync         bool                    // whether the node should statesync on startup
	stateSyncReactor  *statesync.Reactor      // for hosting and restoring statesync snapshots
	stateSyncProvider statesync.StateProvider // provides state data for bootstrapping a node
	stateSyncState    sm.State                // provides the genesis state for statesync
	stateSyncGenDoc   *types.GenesisDoc       // store genesis doc until the statesync is over.
	appInfoResponse   *abcitypes.InfoResponse // if statesync fails, do the handshake and proceed with blocksync.
}

type waitSyncP2PReactor interface {
	p2p.Reactor
	// required by RPC service
	WaitSync() bool
}

type mempoolReactor interface {
	waitSyncP2PReactor
	TryAddTx(tx types.Tx, sender p2p.Peer) (*abcicli.ReqRes, error)
}

// Option sets a parameter for the node.
type Option func(*Node)

// CustomReactors allows you to add custom reactors (name -> p2p.Reactor) to
// the node's Switch.
//
// WARNING: using any name from the below list of the existing reactors will
// result in replacing it with the custom one.
//
//   - MEMPOOL
//   - BLOCKSYNC
//   - CONSENSUS
//   - EVIDENCE
//   - PEX
//   - STATESYNC
func CustomReactors(reactors map[string]p2p.Reactor) Option {
	return func(n *Node) {
		for name, reactor := range reactors {
			if existingReactor := n.sw.Reactor(name); existingReactor != nil {
				n.sw.Logger.Info("Replacing existing reactor with a custom one",
					"name", name, "existing", existingReactor, "custom", reactor)
				n.sw.RemoveReactor(name, existingReactor)
			}
			n.sw.AddReactor(name, reactor)
			// register the new channels to the nodeInfo
			// NOTE: This is a bit messy now with the type casting but is
			// cleaned up in the following version when NodeInfo is changed from
			// and interface to a concrete type
			if ni, ok := n.nodeInfo.(p2p.NodeInfoDefault); ok {
				for _, chDesc := range reactor.StreamDescriptors() {
					if !ni.HasChannel(chDesc.StreamID()) {
						ni.Channels = append(ni.Channels, chDesc.StreamID())
					}
				}
				n.nodeInfo = ni
			} else {
				n.Logger.Error("Node info is not of type p2p.NodeInfoDefault. Custom reactor channels can not be added.")
			}
		}
	}
}

// StateProvider overrides the state provider used by state sync to retrieve trusted app hashes and
// build a State object for bootstrapping the node.
// WARNING: this interface is considered unstable and subject to change.
func StateProvider(stateProvider statesync.StateProvider) Option {
	return func(n *Node) {
		n.stateSyncProvider = stateProvider
	}
}

// BootstrapState synchronizes the stores with the application after state sync
// has been performed offline. It is expected that the block store and state
// store are empty at the time the function is called.
//
// If the block store is not empty, the function returns an error.
func BootstrapState(ctx context.Context, config *cfg.Config, dbProvider cfg.DBProvider, genProvider GenesisDocProvider, height uint64, appHash []byte) (err error) {
	logger := log.NewLogger(os.Stdout)
	if ctx == nil {
		ctx = context.Background()
	}

	if config == nil {
		logger.Info("no config provided, using default configuration")
		config = cfg.DefaultConfig()
	}

	if dbProvider == nil {
		dbProvider = cfg.DefaultDBProvider
	}
	blockStoreDB, stateDB, err := initDBs(config, dbProvider)

	blockStore := store.NewBlockStore(blockStoreDB, store.WithMetrics(store.NopMetrics()), store.WithCompaction(config.Storage.Compact, config.Storage.CompactionInterval), store.WithDBKeyLayout(config.Storage.ExperimentalKeyLayout))
	logger.Info("Blockstore version", "version", blockStore.GetVersion())

	defer func() {
		if derr := blockStore.Close(); derr != nil {
			logger.Error("Failed to close blockstore", "err", derr)
			// Set the return value
			err = derr
		}
	}()

	if err != nil {
		return err
	}

	if !blockStore.IsEmpty() {
		return ErrNonEmptyBlockStore
	}

	stateStore := sm.NewStore(stateDB, sm.StoreOptions{
		DiscardABCIResponses: config.Storage.DiscardABCIResponses,
		Logger:               logger,
		DBKeyLayout:          config.Storage.ExperimentalKeyLayout,
	})

	defer func() {
		if derr := stateStore.Close(); derr != nil {
			logger.Error("Failed to close statestore", "err", derr)
			// Set the return value
			err = derr
		}
	}()
	state, err := stateStore.Load()
	if err != nil {
		return err
	}

	if !state.IsEmpty() {
		return ErrNonEmptyState
	}

	// The state store will use the DBKeyLayout set in config or already existing in the DB.
	genState, _, err := LoadStateFromDBOrGenesisDocProvider(stateDB, genProvider, "")
	if err != nil {
		return err
	}

	stateProvider, err := statesync.NewLightClientStateProviderWithDBKeyVersion(
		ctx,
		genState.ChainID, genState.Version, genState.InitialHeight,
		config.StateSync.RPCServers, light.TrustOptions{
			Period: config.StateSync.TrustPeriod,
			Height: config.StateSync.TrustHeight,
			Hash:   config.StateSync.TrustHashBytes(),
		}, logger.With("module", "light"),
		config.Storage.ExperimentalKeyLayout)
	if err != nil {
		return ErrLightClientStateProvider{Err: err}
	}

	state, err = stateProvider.State(ctx, height)
	if err != nil {
		return err
	}
	if appHash == nil {
		logger.Info("warning: cannot verify appHash. Verification will happen when node boots up!")
	} else if !bytes.Equal(appHash, state.AppHash) {
		if err := blockStore.Close(); err != nil {
			logger.Error("failed to close blockstore: %w", err)
		}
		if err := stateStore.Close(); err != nil {
			logger.Error("failed to close statestore: %w", err)
		}
		return ErrMismatchAppHash{Expected: appHash, Actual: state.AppHash}
	}

	commit, err := stateProvider.Commit(ctx, height)
	if err != nil {
		return err
	}

	if err = stateStore.Bootstrap(state); err != nil {
		return err
	}

	err = blockStore.SaveSeenCommit(state.LastBlockHeight, commit)
	if err != nil {
		return err
	}

	// Once the stores are bootstrapped, we need to set the height at which the node has finished
	// statesyncing. This will allow the blocksync reactor to fetch blocks at a proper height.
	// In case this operation fails, it is equivalent to a failure in  online state sync where the operator
	// needs to manually delete the state and blockstores and rerun the bootstrapping process.
	err = stateStore.SetOfflineStateSyncHeight(state.LastBlockHeight)
	if err != nil {
		return ErrSetSyncHeight{Err: err}
	}

	return err
}

// ------------------------------------------------------------------------------

// NewNode returns a new, ready to go, CometBFT Node.
func NewNode(ctx context.Context,
	config *cfg.Config,
	privValidator types.PrivValidator,
	nodeKey *p2p.NodeKey,
	clientCreator proxy.ClientCreator,
	genesisDocProvider GenesisDocProvider,
	dbProvider cfg.DBProvider,
	metricsProvider MetricsProvider,
	logger log.Logger,
	options ...Option,
) (*Node, error) {
	return NewNodeWithCliParams(ctx,
		config,
		privValidator,
		nodeKey,
		clientCreator,
		genesisDocProvider,
		dbProvider,
		metricsProvider,
		logger,
		CliParams{},
		options...)
}

// NewNodeWithCliParams returns a new, ready to go, CometBFT node
// where we check the hash of the provided genesis file against
// a hash provided by the operator via cli.

func NewNodeWithCliParams(ctx context.Context,
	config *cfg.Config,
	privValidator types.PrivValidator,
	nodeKey *p2p.NodeKey,
	clientCreator proxy.ClientCreator,
	genesisDocProvider GenesisDocProvider,
	dbProvider cfg.DBProvider,
	metricsProvider MetricsProvider,
	logger log.Logger,
	cliParams CliParams,
	options ...Option,
) (*Node, error) {
	blockStoreDB, stateDB, err := initDBs(config, dbProvider)
	if err != nil {
		return nil, err
	}

	var genesisHash string
	if len(cliParams.GenesisHash) != 0 {
		genesisHash = hex.EncodeToString(cliParams.GenesisHash)
	}
	state, genDoc, err := LoadStateFromDBOrGenesisDocProvider(stateDB, genesisDocProvider, genesisHash)
	if err != nil {
		return nil, err
	}

	csMetrics, p2pMetrics, memplMetrics, smMetrics, bstMetrics, abciMetrics, bsMetrics, ssMetrics := metricsProvider(genDoc.ChainID)
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{
		DiscardABCIResponses: config.Storage.DiscardABCIResponses,
		Metrics:              smMetrics,
		Compact:              config.Storage.Compact,
		CompactionInterval:   config.Storage.CompactionInterval,
		Logger:               logger,
		DBKeyLayout:          config.Storage.ExperimentalKeyLayout,
	})

	blockStore := store.NewBlockStore(blockStoreDB, store.WithMetrics(bstMetrics), store.WithCompaction(config.Storage.Compact, config.Storage.CompactionInterval), store.WithDBKeyLayout(config.Storage.ExperimentalKeyLayout))
	logger.Info("Blockstore version", "version", blockStore.GetVersion())

	// The key will be deleted if it existed.
	// Not checking whether the key is there in case the genesis file was larger than
	// the max size of a value (in rocksDB for example), which would cause the check
	// to fail and prevent the node from booting.
	logger.Warn("deleting genesis file from database if present, the database stores a hash of the original genesis file now")

	err = stateDB.Delete(genesisDocKey)
	if err != nil {
		logger.Error("Failed to delete genesis doc from DB ", err)
	}

	// Create the proxyApp and establish connections to the ABCI app (consensus, mempool, query).
	proxyApp, err := createAndStartProxyAppConns(clientCreator, logger, abciMetrics)
	if err != nil {
		return nil, err
	}

	// EventBus and IndexerService must be started before the handshake because
	// we might need to index the txs of the replayed block as this might not have happened
	// when the node stopped last time (i.e. the node stopped after it saved the block
	// but before it indexed the txs)
	eventBus, err := createAndStartEventBus(logger)
	if err != nil {
		return nil, err
	}

	indexerService, txIndexer, blockIndexer, err := createAndStartIndexerService(config,
		genDoc.ChainID, dbProvider, eventBus, logger)
	if err != nil {
		return nil, err
	}

	// If an address is provided, listen on the socket for a connection from an
	// external signing process.
	if config.PrivValidatorListenAddr != "" {
		// FIXME: we should start services inside OnStart
		privValidator, err = createAndStartPrivValidatorSocketClient(config.PrivValidatorListenAddr, genDoc.ChainID, logger)
		if err != nil {
			return nil, ErrPrivValidatorSocketClient{Err: err}
		}
	}

	pubKey, err := privValidator.GetPubKey()
	if err != nil {
		return nil, ErrGetPubKey{Err: err}
	}
	localAddr := pubKey.Address()

	// Determine whether we should attempt state sync.
	stateSync := config.StateSync.Enable && !state.Validators.ValidatorBlocksTheChain(localAddr)
	if stateSync && state.LastBlockHeight > 0 {
		logger.Info("Found local state with non-zero height, skipping state sync")
		stateSync = false
	}

	// Create the handshaker, which calls RequestInfo, sets the AppVersion on the state,
	// and replays any blocks as necessary to sync CometBFT with the app.
	consensusLogger := logger.With("module", "consensus")

	appInfoResponse, err := proxyApp.Query().Info(ctx, proxy.InfoRequest)
	if err != nil {
		return nil, fmt.Errorf("error calling ABCI Info method: %v", err)
	}

	if !stateSync {
		if err := doHandshake(ctx, stateStore, state, blockStore, genDoc, eventBus, appInfoResponse, proxyApp, consensusLogger); err != nil {
			return nil, ErrHandshake{Err: err}
		}

		// Reload the state. It will have the Version.Consensus.App set by the
		// Handshake, and may have other modifications as well (ie. depending on
		// what happened during block replay).
		state, err = stateStore.Load()
		if err != nil {
			return nil, sm.ErrCannotLoadState{Err: err}
		}
	}

	logNodeStartupInfo(state, pubKey, logger, consensusLogger)

	// Blocksync is always active, except if the local node blocks the chain
	waitSync := !state.Validators.ValidatorBlocksTheChain(localAddr)

	mempool, mempoolReactor := createMempoolAndMempoolReactor(config, proxyApp, state, eventBus, waitSync, memplMetrics, logger, appInfoResponse)

	evidenceReactor, evidencePool, err := createEvidenceReactor(config, dbProvider, stateStore, blockStore, logger)
	if err != nil {
		return nil, err
	}

	pruner, err := createPruner(
		config,
		txIndexer,
		blockIndexer,
		stateStore,
		blockStore,
		smMetrics,
		logger.With("module", "state"),
	)
	if err != nil {
		return nil, ErrCreatePruner{Err: err}
	}

	// make block executor for consensus and blocksync reactors to execute blocks
	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger.With("module", "state"),
		proxyApp.Consensus(),
		mempool,
		evidencePool,
		blockStore,
		sm.BlockExecutorWithPruner(pruner),
		sm.BlockExecutorWithMetrics(smMetrics),
	)

	offlineStateSyncHeight := int64(0)
	if blockStore.Height() == 0 {
		offlineStateSyncHeight, err = blockExec.Store().GetOfflineStateSyncHeight()
		if err != nil && err.Error() != "value empty" {
			panic(fmt.Sprintf("failed to retrieve statesynced height from store %s; expected state store height to be %v", err, state.LastBlockHeight))
		}
	}
	// Don't start block sync if we're doing a state sync first, or we are blocking the chain.
	blockSync := !stateSync && !state.Validators.ValidatorBlocksTheChain(localAddr)
	bcReactor, err := createBlocksyncReactor(config, state, blockExec, blockStore, blockSync, localAddr, logger, bsMetrics, offlineStateSyncHeight)
	if err != nil {
		return nil, ErrCreateBlockSyncReactor{Err: err}
	}

	consensusReactor, consensusState := createConsensusReactor(
		config, state, blockExec, blockStore, mempool, evidencePool,
		privValidator, csMetrics, waitSync, eventBus, consensusLogger, offlineStateSyncHeight,
	)

	err = stateStore.SetOfflineStateSyncHeight(0)
	if err != nil {
		panic(fmt.Sprintf("failed to reset the offline state sync height %s", err))
	}
	// Set up state sync reactor, and schedule a sync if requested.
	// FIXME The way we do phased startups (e.g. replay -> block sync -> consensus) is very messy,
	// we should clean this whole thing up. See:
	// https://github.com/tendermint/tendermint/issues/4644
	stateSyncReactor := statesync.NewReactor(
		*config.StateSync,
		proxyApp.Snapshot(),
		proxyApp.Query(),
		ssMetrics,
	)
	stateSyncReactor.SetLogger(logger.With("module", "statesync"))

	nodeInfo, err := makeNodeInfo(config, nodeKey, txIndexer, genDoc, state)
	if err != nil {
		return nil, err
	}

	transport, peerFilters := createTransport(config, nodeKey, proxyApp)

	p2pLogger := logger.With("module", "p2p")
	transport.SetLogger(p2pLogger)

	sw := createSwitch(
		config, transport, p2pMetrics, peerFilters, mempoolReactor, bcReactor,
		stateSyncReactor, consensusReactor, evidenceReactor, nodeInfo, nodeKey, p2pLogger,
	)

	err = sw.AddPersistentPeers(splitAndTrimEmpty(config.P2P.PersistentPeers, ",", " "))
	if err != nil {
		return nil, ErrAddPersistentPeers{Err: err}
	}

	err = sw.AddUnconditionalPeerIDs(splitAndTrimEmpty(config.P2P.UnconditionalPeerIDs, ",", " "))
	if err != nil {
		return nil, ErrAddUnconditionalPeerIDs{Err: err}
	}

	addrBook, err := createAddrBookAndSetOnSwitch(config, sw, p2pLogger, nodeKey)
	if err != nil {
		return nil, ErrCreateAddrBook{Err: err}
	}

	// Optionally, start the pex reactor
	//
	// TODO:
	//
	// We need to set Seeds and PersistentPeers on the switch,
	// since it needs to be able to use these (and their DNS names)
	// even if the PEX is off. We can include the DNS name in the na.NetAddr,
	// but it would still be nice to have a clear list of the current "PersistentPeers"
	// somewhere that we can return with net_info.
	//
	// If PEX is on, it should handle dialing the seeds. Otherwise the switch does it.
	// Note we currently use the addrBook regardless at least for AddOurAddress
	var pexReactor *pex.Reactor
	if config.P2P.PexReactor {
		pexReactor = createPEXReactorAndAddToSwitch(addrBook, config, sw, logger)
	}

	// Add private IDs to addrbook to block those peers being added
	addrBook.AddPrivateIDs(splitAndTrimEmpty(config.P2P.PrivatePeerIDs, ",", " "))

	node := &Node{
		config:        config,
		genesisTime:   genDoc.GenesisTime,
		privValidator: privValidator,

		transport: transport,
		sw:        sw,
		addrBook:  addrBook,
		nodeInfo:  nodeInfo,
		nodeKey:   nodeKey,

		stateStore:       stateStore,
		blockStore:       blockStore,
		pruner:           pruner,
		bcReactor:        bcReactor,
		mempoolReactor:   mempoolReactor,
		mempool:          mempool,
		consensusState:   consensusState,
		consensusReactor: consensusReactor,
		pexReactor:       pexReactor,
		evidencePool:     evidencePool,
		proxyApp:         proxyApp,
		txIndexer:        txIndexer,
		indexerService:   indexerService,
		blockIndexer:     blockIndexer,
		eventBus:         eventBus,

		// statesync
		stateSync:        stateSync,
		stateSyncReactor: stateSyncReactor,
		stateSyncState:   state,
		stateSyncGenDoc:  genDoc,
		appInfoResponse:  appInfoResponse,
	}
	node.BaseService = *service.NewBaseService(logger, "Node", node)

	for _, option := range options {
		option(node)
	}

	return node, nil
}

// OnStart starts the Node. It implements service.Service.
func (n *Node) OnStart() error {
	now := cmttime.Now()
	genTime := n.genesisTime
	if genTime.After(now) {
		n.Logger.Info("Genesis time is in the future. Sleeping until then...", "genTime", genTime)
		time.Sleep(genTime.Sub(now))
	}

	// run pprof server if it is enabled
	if n.config.RPC.IsPprofEnabled() {
		n.pprofSrv = n.startPprofServer()
	}

	// begin prometheus metrics gathering if it is enabled
	if n.config.Instrumentation.IsPrometheusEnabled() {
		n.prometheusSrv = n.startPrometheusServer()
	}

	// Start the RPC server before the P2P server
	// so we can eg. receive txs for the first block
	if n.config.RPC.ListenAddress != "" {
		listeners, err := n.startRPC()
		if err != nil {
			return fmt.Errorf("starting RPC server: %w", err)
		}
		n.rpcListeners = listeners
	}

	// Start the transport.
	addr, err := na.NewFromString(na.IDAddrString(n.nodeKey.ID(), n.config.P2P.ListenAddress))
	if err != nil {
		return err
	}
	if err := n.transport.Listen(*addr); err != nil {
		return err
	}

	n.isListening = true

	// Start the switch (the P2P server).
	err = n.sw.Start()
	if err != nil {
		return err
	}

	// Always connect to persistent peers
	err = n.sw.DialPeersAsync(splitAndTrimEmpty(n.config.P2P.PersistentPeers, ",", " "))
	if err != nil {
		return ErrDialPeers{Err: err}
	}

	// Start statesync.
	if n.stateSync {
		bcR, ok := n.bcReactor.(blockSyncReactor)
		if !ok {
			return ErrSwitchStateSync
		}

		stateCh, errc, err := startStateSync(n.stateSyncReactor, n.stateSyncProvider,
			n.config.StateSync, n.stateStore, n.blockStore, n.stateSyncState, n.config.Storage.ExperimentalKeyLayout)
		if err != nil {
			return ErrStartStateSync{Err: err}
		}

		// Wait until statesync is over or errors, and start blocksync in a separate goroutine.
		go func() {
			defer func() {
				// FREE GENESIS DOC MEMORY!
				n.stateSyncGenDoc = nil
				n.appInfoResponse = nil
			}()

			select {
			case <-n.Quit():
				return
			case newState := <-stateCh:
				n.Logger.Info("statesync complete. switching to blocksync with new state")

				err = bcR.SwitchToBlockSync(newState)
				if err != nil {
					n.Logger.Error("failed to switch to blocksync", "err", err)
					return
				}
			case err := <-errc:
				n.Logger.Warn("statesync failed. switching to blocksync with genesis state", "err", err)

				const handshakeTimeout = 5 * time.Second
				ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
				defer cancel()
				if err := doHandshake(ctx, n.stateStore, n.stateSyncState, n.blockStore,
					n.stateSyncGenDoc, n.eventBus, n.appInfoResponse, n.proxyApp, n.Logger); err != nil {
					n.Logger.Error("failed to handshake with the app", "err", err)
					return
				}

				// Reload the state. It will have the Version.Consensus.App set by the
				// Handshake, and may have other modifications as well (ie. depending on
				// what happened during block replay).
				state, err := n.stateStore.Load()
				if err != nil {
					n.Logger.Error("failed to load state", "err", err)
					return
				}

				err = bcR.SwitchToBlockSync(state)
				if err != nil {
					n.Logger.Error("failed to switch to blocksync", "err", err)
					return
				}
			}
		}()
	}

	// Start background pruning
	if err := n.pruner.Start(); err != nil {
		return ErrStartPruning{Err: err}
	}

	return nil
}

// OnStop stops the Node. It implements service.Service.
func (n *Node) OnStop() {
	n.BaseService.OnStop()

	n.Logger.Info("Stopping Node")

	// first stop the non-reactor services
	if err := n.pruner.Stop(); err != nil {
		n.Logger.Error("Error stopping the pruning service", "err", err)
	}
	if err := n.eventBus.Stop(); err != nil {
		n.Logger.Error("Error closing eventBus", "err", err)
	}
	if n.indexerService != nil {
		if err := n.indexerService.Stop(); err != nil {
			n.Logger.Error("Error closing indexerService", "err", err)
		}
	}
	// now stop the reactors
	if err := n.sw.Stop(); err != nil {
		n.Logger.Error("Error closing switch", "err", err)
	}

	if err := n.transport.Close(); err != nil {
		n.Logger.Error("Error closing transport", "err", err)
	}

	n.isListening = false

	// finally stop the listeners / external services
	for _, l := range n.rpcListeners {
		n.Logger.Info("Closing rpc listener", "listener", l)
		if err := l.Close(); err != nil {
			n.Logger.Error("Error closing listener", "listener", l, "err", err)
		}
	}

	if pvsc, ok := n.privValidator.(service.Service); ok {
		if err := pvsc.Stop(); err != nil {
			n.Logger.Error("Error closing private validator", "err", err)
		}
	}

	if n.prometheusSrv != nil {
		if err := n.prometheusSrv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout:
			n.Logger.Error("Prometheus HTTP server Shutdown", "err", err)
		}
	}
	if n.pprofSrv != nil {
		if err := n.pprofSrv.Shutdown(context.Background()); err != nil {
			n.Logger.Error("Pprof HTTP server Shutdown", "err", err)
		}
	}
	if n.blockStore != nil {
		n.Logger.Info("Closing blockstore")
		if err := n.blockStore.Close(); err != nil {
			n.Logger.Error("problem closing blockstore", "err", err)
		}
	}
	if n.stateStore != nil {
		n.Logger.Info("Closing statestore")
		if err := n.stateStore.Close(); err != nil {
			n.Logger.Error("problem closing statestore", "err", err)
		}
	}
	if n.evidencePool != nil {
		n.Logger.Info("Closing evidencestore")
		if err := n.EvidencePool().Close(); err != nil {
			n.Logger.Error("problem closing evidencestore", "err", err)
		}
	}
}

// ConfigureRPC initializes and returns an `Environment` object with all the data
// it needs to serve the RPC APIs.
func (n *Node) ConfigureRPC() (*rpccore.Environment, error) {
	pubKey, err := n.privValidator.GetPubKey()
	if pubKey == nil || err != nil {
		return nil, ErrGetPubKey{Err: err}
	}

	rpcEnv := &rpccore.Environment{
		ProxyAppQuery:   n.proxyApp.Query(),
		ProxyAppMempool: n.proxyApp.Mempool(),

		StateStore:     n.stateStore,
		BlockStore:     n.blockStore,
		EvidencePool:   n.evidencePool,
		ConsensusState: n.consensusState,
		P2PPeers:       n.sw,
		P2PTransport:   n,
		PubKey:         pubKey,

		TxIndexer:        n.txIndexer,
		BlockIndexer:     n.blockIndexer,
		ConsensusReactor: n.consensusReactor,
		MempoolReactor:   n.mempoolReactor,
		EventBus:         n.eventBus,
		Mempool:          n.mempool,

		Logger: n.Logger.With("module", "rpc"),

		Config: *n.config.RPC,

		GenesisFilePath: n.config.GenesisFile(),
	}

	n.Logger.Info("Creating genesis file chunks if genesis file is too big...")
	if err := rpcEnv.InitGenesisChunks(); err != nil {
		return nil, fmt.Errorf("configuring RPC API environment: %w", err)
	}

	return rpcEnv, nil
}

func (n *Node) startRPC() ([]net.Listener, error) {
	env, err := n.ConfigureRPC()
	if err != nil {
		return nil, fmt.Errorf("configuring RPC server: %s", err)
	}

	listenAddrs := splitAndTrimEmpty(n.config.RPC.ListenAddress, ",", " ")
	routes := env.GetRoutes()

	if n.config.RPC.Unsafe {
		env.AddUnsafeRoutes(routes)
	}

	config := rpcserver.DefaultConfig()
	config.MaxRequestBatchSize = n.config.RPC.MaxRequestBatchSize
	config.MaxBodyBytes = n.config.RPC.MaxBodyBytes
	config.MaxHeaderBytes = n.config.RPC.MaxHeaderBytes
	config.MaxOpenConnections = n.config.RPC.MaxOpenConnections
	// If necessary adjust global WriteTimeout to ensure it's greater than
	// TimeoutBroadcastTxCommit.
	// See https://github.com/tendermint/tendermint/issues/3435
	if config.WriteTimeout <= n.config.RPC.TimeoutBroadcastTxCommit {
		config.WriteTimeout = n.config.RPC.TimeoutBroadcastTxCommit + 1*time.Second
	}

	// we may expose the rpc over both a unix and tcp socket
	listeners := make([]net.Listener, 0, len(listenAddrs))
	for _, listenAddr := range listenAddrs {
		mux := http.NewServeMux()
		rpcLogger := n.Logger.With("module", "rpc-server")
		wmLogger := rpcLogger.With("protocol", "websocket")
		wm := rpcserver.NewWebsocketManager(routes,
			rpcserver.OnDisconnect(func(remoteAddr string) {
				err := n.eventBus.UnsubscribeAll(context.Background(), remoteAddr)
				if err != nil && err != cmtpubsub.ErrSubscriptionNotFound {
					wmLogger.Error("Failed to unsubscribe addr from events", "addr", remoteAddr, "err", err)
				}
			}),
			rpcserver.ReadLimit(config.MaxBodyBytes),
			rpcserver.WriteChanCapacity(n.config.RPC.WebSocketWriteBufferSize),
		)
		wm.SetLogger(wmLogger)
		mux.HandleFunc("/websocket", wm.WebsocketHandler)
		mux.HandleFunc("/v1/websocket", wm.WebsocketHandler)
		rpcserver.RegisterRPCFuncs(mux, routes, rpcLogger)
		listener, err := rpcserver.Listen(
			listenAddr,
			config.MaxOpenConnections,
		)
		if err != nil {
			return nil, err
		}

		var rootHandler http.Handler = mux
		if n.config.RPC.IsCorsEnabled() {
			corsMiddleware := cors.New(cors.Options{
				AllowedOrigins: n.config.RPC.CORSAllowedOrigins,
				AllowedMethods: n.config.RPC.CORSAllowedMethods,
				AllowedHeaders: n.config.RPC.CORSAllowedHeaders,
			})
			rootHandler = corsMiddleware.Handler(mux)
		}
		if n.config.RPC.IsTLSEnabled() {
			go func() {
				err := rpcserver.ServeTLSWithShutdown(
					listener,
					rootHandler,
					n.config.RPC.CertFile(),
					n.config.RPC.KeyFile(),
					rpcLogger,
					config,
					env.Cleanup,
				)
				if err != nil {
					n.Logger.Error("serving server with TLS", "err", err)
				}
			}()
		} else {
			go func() {
				err := rpcserver.ServeWithShutdown(
					listener,
					rootHandler,
					rpcLogger,
					config,
					env.Cleanup,
				)
				if err != nil {
					n.Logger.Error("Error serving server", "err", err)
				}
			}()
		}

		listeners = append(listeners, listener)
	}

	if n.config.GRPC.ListenAddress != "" {
		listener, err := grpcserver.Listen(n.config.GRPC.ListenAddress)
		if err != nil {
			return nil, err
		}
		opts := []grpcserver.Option{
			grpcserver.WithLogger(n.Logger),
		}
		if n.config.GRPC.VersionService.Enabled {
			opts = append(opts, grpcserver.WithVersionService())
		}
		if n.config.GRPC.BlockService.Enabled {
			opts = append(opts, grpcserver.WithBlockService(n.blockStore, n.eventBus, n.Logger))
		}
		if n.config.GRPC.BlockResultsService.Enabled {
			opts = append(opts, grpcserver.WithBlockResultsService(n.blockStore, n.stateStore, n.Logger))
		}
		go func() {
			if err := grpcserver.Serve(listener, opts...); err != nil {
				n.Logger.Error("Error starting gRPC server", "err", err)
			}
		}()
		listeners = append(listeners, listener)
	}

	if n.config.GRPC.Privileged.ListenAddress != "" {
		listener, err := grpcserver.Listen(n.config.GRPC.Privileged.ListenAddress)
		if err != nil {
			return nil, err
		}
		opts := []grpcprivserver.Option{
			grpcprivserver.WithLogger(n.Logger),
		}
		if n.config.GRPC.Privileged.PruningService.Enabled {
			opts = append(opts, grpcprivserver.WithPruningService(n.pruner, n.Logger))
		}
		go func() {
			if err := grpcprivserver.Serve(listener, opts...); err != nil {
				n.Logger.Error("Error starting privileged gRPC server", "err", err)
			}
		}()
		listeners = append(listeners, listener)
	}

	return listeners, nil
}

// startPrometheusServer starts a Prometheus HTTP server, listening for metrics
// collectors on addr.
func (n *Node) startPrometheusServer() *http.Server {
	srv := &http.Server{
		Addr: n.config.Instrumentation.PrometheusListenAddr,
		Handler: promhttp.InstrumentMetricHandler(
			prometheus.DefaultRegisterer, promhttp.HandlerFor(
				prometheus.DefaultGatherer,
				promhttp.HandlerOpts{MaxRequestsInFlight: n.config.Instrumentation.MaxOpenConnections},
			),
		),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// Error starting or closing listener:
			n.Logger.Error("Prometheus HTTP server ListenAndServe", "err", err)
		}
	}()
	return srv
}

// starts a ppro.
func (n *Node) startPprofServer() *http.Server {
	srv := &http.Server{
		Addr:              n.config.RPC.PprofListenAddress,
		Handler:           nil,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// Error starting or closing listener:
			n.Logger.Error("pprof HTTP server ListenAndServe", "err", err)
		}
	}()
	return srv
}

// Switch returns the Node's Switch.
func (n *Node) Switch() *p2p.Switch {
	return n.sw
}

// BlockStore returns the Node's BlockStore.
func (n *Node) BlockStore() *store.BlockStore {
	return n.blockStore
}

// ConsensusReactor returns the Node's ConsensusReactor.
func (n *Node) ConsensusReactor() *cs.Reactor {
	return n.consensusReactor
}

// MempoolReactor returns the Node's mempool reactor.
func (n *Node) MempoolReactor() p2p.Reactor {
	return n.mempoolReactor
}

// Mempool returns the Node's mempool.
func (n *Node) Mempool() mempl.Mempool {
	return n.mempool
}

// PEXReactor returns the Node's PEXReactor. It returns nil if PEX is disabled.
func (n *Node) PEXReactor() *pex.Reactor {
	return n.pexReactor
}

// EvidencePool returns the Node's EvidencePool.
func (n *Node) EvidencePool() *evidence.Pool {
	return n.evidencePool
}

// EventBus returns the Node's EventBus.
func (n *Node) EventBus() *types.EventBus {
	return n.eventBus
}

// PrivValidator returns the Node's PrivValidator.
// XXX: for convenience only!
func (n *Node) PrivValidator() types.PrivValidator {
	return n.privValidator
}

// GenesisDoc returns a GenesisDoc object after reading the genesis file from disk.
// The function does not check for the genesis's validity since it was already
// checked at startup, and we work under the assumption that correct nodes (i.e.,
// non-Byzantine) are not compromised. Therefore, their file system can be
// trusted while the node is running.
// Note that the genesis file can be large (hundreds of MBs, even GBs); therefore,
// we recommend that the caller does not keep the GenesisDoc returned by this
// function in memory longer than necessary.
func (n *Node) GenesisDoc() (*types.GenesisDoc, error) {
	gDocPath := n.config.GenesisFile()

	gDocJSON, err := os.ReadFile(gDocPath)
	if err != nil {
		return nil, fmt.Errorf("unavailable genesis file at %s: %w", gDocPath, err)
	}

	var gDoc types.GenesisDoc

	err = cmtjson.Unmarshal(gDocJSON, &gDoc)
	if err != nil {
		formatStr := "invalid JSON format for genesis file at %s: %w"
		return nil, fmt.Errorf(formatStr, gDocPath, err)
	}

	return &gDoc, nil
}

// ProxyApp returns the Node's AppConns, representing its connections to the ABCI application.
func (n *Node) ProxyApp() proxy.AppConns {
	return n.proxyApp
}

// Config returns the Node's config.
func (n *Node) Config() *cfg.Config {
	return n.config
}

// ------------------------------------------------------------------------------

func (n *Node) Listeners() []string {
	return []string{
		fmt.Sprintf("Listener(@%v)", n.config.P2P.ExternalAddress),
	}
}

func (n *Node) IsListening() bool {
	return n.isListening
}

// NodeInfo returns the Node's Info from the Switch.
func (n *Node) NodeInfo() p2p.NodeInfo {
	return n.nodeInfo
}

func makeNodeInfo(
	config *cfg.Config,
	nodeKey *p2p.NodeKey,
	txIndexer txindex.TxIndexer,
	genDoc *types.GenesisDoc,
	state sm.State,
) (p2p.NodeInfoDefault, error) {
	txIndexerStatus := "on"
	if _, ok := txIndexer.(*null.TxIndex); ok {
		txIndexerStatus = "off"
	}

	nodeInfo := p2p.NodeInfoDefault{
		ProtocolVersion: p2p.ProtocolVersion{
			P2P:   version.P2PProtocol, // global
			Block: state.Version.Consensus.Block,
			App:   state.Version.Consensus.App,
		},
		DefaultNodeID: nodeKey.ID(),
		Network:       genDoc.ChainID,
		Version:       version.CMTSemVer,
		Channels: []byte{
			bc.BlocksyncChannel,
			cs.StateChannel, cs.DataChannel, cs.VoteChannel, cs.VoteSetBitsChannel,
			mempl.MempoolChannel, mempl.MempoolControlChannel,
			evidence.EvidenceChannel,
			statesync.SnapshotChannel, statesync.ChunkChannel,
		},
		Moniker: config.Moniker,
		Other: p2p.NodeInfoDefaultOther{
			TxIndex:    txIndexerStatus,
			RPCAddress: config.RPC.ListenAddress,
		},
	}

	if config.P2P.PexReactor {
		nodeInfo.Channels = append(nodeInfo.Channels, pex.PexChannel)
	}

	lAddr := config.P2P.ExternalAddress

	if lAddr == "" {
		lAddr = config.P2P.ListenAddress
	}

	nodeInfo.ListenAddr = lAddr

	err := nodeInfo.Validate()
	return nodeInfo, err
}

func createPruner(
	config *cfg.Config,
	txIndexer txindex.TxIndexer,
	blockIndexer indexer.BlockIndexer,
	stateStore sm.Store,
	blockStore *store.BlockStore,
	metrics *sm.Metrics,
	logger log.Logger,
) (*sm.Pruner, error) {
	if err := initApplicationRetainHeight(stateStore); err != nil {
		return nil, err
	}

	prunerOpts := []sm.PrunerOption{
		sm.WithPrunerInterval(config.Storage.Pruning.Interval),
		sm.WithPrunerMetrics(metrics),
	}

	if config.Storage.Pruning.DataCompanion.Enabled {
		err := initCompanionRetainHeights(
			stateStore,
			config.Storage.Pruning.DataCompanion.InitialBlockRetainHeight,
			config.Storage.Pruning.DataCompanion.InitialBlockResultsRetainHeight,
		)
		if err != nil {
			return nil, err
		}
		prunerOpts = append(prunerOpts, sm.WithPrunerCompanionEnabled())
	}

	return sm.NewPruner(stateStore, blockStore, blockIndexer, txIndexer, logger, prunerOpts...), nil
}

// Set the initial application retain height to 0 to avoid the data companion
// pruning blocks before the application indicates it is OK. We set this to 0
// only if the retain height was not set before by the application.
func initApplicationRetainHeight(stateStore sm.Store) error {
	if _, err := stateStore.GetApplicationRetainHeight(); err != nil {
		if errors.Is(err, sm.ErrKeyNotFound) {
			return stateStore.SaveApplicationRetainHeight(0)
		}
		return err
	}
	return nil
}

// Sets the data companion retain heights if one of two possible conditions is
// met:
// 1. One or more of the retain heights has not yet been set.
// 2. One or more of the retain heights is currently 0.
func initCompanionRetainHeights(stateStore sm.Store, initBlockRH, initBlockResultsRH int64) error {
	curBlockRH, err := stateStore.GetCompanionBlockRetainHeight()
	if err != nil && !errors.Is(err, sm.ErrKeyNotFound) {
		return fmt.Errorf("failed to obtain companion block retain height: %w", err)
	}
	if curBlockRH == 0 {
		if err := stateStore.SaveCompanionBlockRetainHeight(initBlockRH); err != nil {
			return fmt.Errorf("failed to set initial data companion block retain height: %w", err)
		}
	}
	curBlockResultsRH, err := stateStore.GetABCIResRetainHeight()
	if err != nil && !errors.Is(err, sm.ErrKeyNotFound) {
		return fmt.Errorf("failed to obtain companion block results retain height: %w", err)
	}
	if curBlockResultsRH == 0 {
		if err := stateStore.SaveABCIResRetainHeight(initBlockResultsRH); err != nil {
			return fmt.Errorf("failed to set initial data companion block results retain height: %w", err)
		}
	}
	return nil
}
