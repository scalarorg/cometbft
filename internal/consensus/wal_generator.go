package consensus

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	db "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/v2/abci/example/kvstore"
	cfg "github.com/cometbft/cometbft/v2/config"
	cmtrand "github.com/cometbft/cometbft/v2/internal/rand"
	"github.com/cometbft/cometbft/v2/libs/log"
	"github.com/cometbft/cometbft/v2/privval"
	"github.com/cometbft/cometbft/v2/proxy"
	sm "github.com/cometbft/cometbft/v2/state"
	"github.com/cometbft/cometbft/v2/store"
	"github.com/cometbft/cometbft/v2/types"
)

// WALGenerateNBlocks generates a consensus WAL. It does this by spinning up a
// stripped down version of node (proxy app, event bus, consensus state) with a
// persistent kvstore application and special consensus wal instance
// (byteBufferWAL) and waits until numBlocks are created.
// If the node fails to produce given numBlocks, it returns an error.
func WALGenerateNBlocks(t *testing.T, wr io.Writer, numBlocks int, config *cfg.Config) (err error) {
	t.Helper()

	app := kvstore.NewPersistentApplication(filepath.Join(config.DBDir(), "wal_generator"))

	logger := log.TestingLogger().With("wal_generator", "wal_generator")
	logger.Info("generating WAL (last height msg excluded)", "numBlocks", numBlocks)

	// COPY PASTE FROM node.go WITH A FEW MODIFICATIONS
	// NOTE: we can't import node package because of circular dependency.
	// NOTE: we don't do handshake so need to set state.Version.Consensus.App directly.
	privValidatorKeyFile := config.PrivValidatorKeyFile()
	privValidatorStateFile := config.PrivValidatorStateFile()
	privValidator, err := privval.LoadOrGenFilePV(privValidatorKeyFile, privValidatorStateFile, nil)
	require.NoError(t, err)
	genDoc, err := types.GenesisDocFromFile(config.GenesisFile())
	require.NoError(t, err)
	blockStoreDB := db.NewMemDB()
	stateDB := blockStoreDB
	stateStore := sm.NewStore(stateDB, sm.StoreOptions{
		DiscardABCIResponses: false,
	})
	state, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err, "failed to make genesis state")
	state.Version.Consensus.App = kvstore.AppVersion
	err = stateStore.Save(state)
	require.NoError(t, err)

	blockStore := store.NewBlockStore(blockStoreDB)

	proxyApp := proxy.NewAppConns(proxy.NewLocalClientCreator(app), proxy.NopMetrics())
	proxyApp.SetLogger(logger.With("module", "proxy"))
	err = proxyApp.Start()
	require.NoError(t, err, "failed to start proxy app connections")
	t.Cleanup(func() {
		if err := proxyApp.Stop(); err != nil {
			t.Error(err)
		}
	})

	eventBus := types.NewEventBus()
	eventBus.SetLogger(logger.With("module", "events"))
	err = eventBus.Start()
	require.NoError(t, err, "failed to start event bus")
	t.Cleanup(func() {
		if err := eventBus.Stop(); err != nil {
			t.Error(err)
		}
	})
	mempool := emptyMempool{}
	evpool := sm.EmptyEvidencePool{}
	blockExec := sm.NewBlockExecutor(stateStore, log.TestingLogger(), proxyApp.Consensus(), mempool, evpool, blockStore)
	consensusState := NewState(config.Consensus, state.Copy(), blockExec, blockStore, mempool, evpool)
	consensusState.SetLogger(logger)
	consensusState.SetEventBus(eventBus)
	if privValidator != nil {
		consensusState.SetPrivValidator(privValidator)
	}
	// END OF COPY PASTE

	// set consensus wal to buffered WAL, which will write all incoming msgs to buffer
	numBlocksWritten := make(chan struct{})
	wal := newByteBufferWAL(logger, NewWALEncoder(wr), int64(numBlocks), numBlocksWritten)
	// see wal.go#103
	err = wal.Write(EndHeightMessage{0})
	require.NoError(t, err)

	consensusState.wal = wal

	err = consensusState.Start()
	require.NoError(t, err, "failed to start consensus state")

	select {
	case <-numBlocksWritten:
		err := consensusState.Stop()
		require.NoError(t, err)
		return nil
	case <-time.After(1 * time.Minute):
		err := consensusState.Stop()
		require.NoError(t, err)
		return fmt.Errorf("waited too long for CometBFT to produce %d blocks (grep logs for `wal_generator`)", numBlocks)
	}
}

// WALWithNBlocks returns a WAL content with numBlocks.
func WALWithNBlocks(t *testing.T, numBlocks int, config *cfg.Config) (data []byte, err error) {
	t.Helper()

	var b bytes.Buffer
	wr := bufio.NewWriter(&b)

	if err := WALGenerateNBlocks(t, wr, numBlocks, config); err != nil {
		return []byte{}, err
	}

	wr.Flush()
	return b.Bytes(), nil
}

func randPort() int {
	// returns between base and base + spread
	base, spread := 20000, 20000
	return base + cmtrand.Intn(spread)
}

func makeAddrs() (address, port string) {
	start := randPort()
	return fmt.Sprintf("tcp://127.0.0.1:%d", start),
		fmt.Sprintf("tcp://127.0.0.1:%d", start+1)
}

// byteBufferWAL is a WAL which writes all msgs to a byte buffer. Writing stops
// when the heightToStop is reached. Client will be notified via
// signalWhenStopsTo channel.
type byteBufferWAL struct {
	enc               *WALEncoder
	stopped           bool
	heightToStop      int64
	signalWhenStopsTo chan<- struct{}

	logger log.Logger
}

// needed for determinism.
var fixedTime, _ = time.Parse(time.RFC3339, "2017-01-02T15:04:05Z")

func newByteBufferWAL(logger log.Logger, enc *WALEncoder, nBlocks int64, signalStop chan<- struct{}) *byteBufferWAL {
	return &byteBufferWAL{
		enc:               enc,
		heightToStop:      nBlocks,
		signalWhenStopsTo: signalStop,
		logger:            logger,
	}
}

// Save writes message to the internal buffer except when heightToStop is
// reached, in which case it will signal the caller via signalWhenStopsTo and
// skip writing.
func (w *byteBufferWAL) Write(m WALMessage) error {
	if w.stopped {
		w.logger.Debug("WAL already stopped. Not writing message", "msg", m)
		return nil
	}

	if endMsg, ok := m.(EndHeightMessage); ok {
		w.logger.Debug("WAL write end height message", "height", endMsg.Height, "stopHeight", w.heightToStop)
		if endMsg.Height == w.heightToStop {
			w.logger.Debug("Stopping WAL at height", "height", endMsg.Height)
			w.signalWhenStopsTo <- struct{}{}
			w.stopped = true
			return nil
		}
	}

	w.logger.Debug("WAL Write Message", "msg", m)
	err := w.enc.Encode(&TimedWALMessage{fixedTime, m})
	if err != nil {
		panic(fmt.Sprintf("failed to encode the msg %v", m))
	}

	return nil
}

func (w *byteBufferWAL) WriteSync(m WALMessage) error {
	return w.Write(m)
}

func (*byteBufferWAL) FlushAndSync() error { return nil }

func (*byteBufferWAL) SearchForEndHeight(
	int64,
	*WALSearchOptions,
) (rd io.ReadCloser, found bool, err error) {
	return nil, false, nil
}

func (*byteBufferWAL) Start() error { return nil }
func (*byteBufferWAL) Stop() error  { return nil }
func (*byteBufferWAL) Wait()        {}
