package api

import (
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/log"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	cfg "github.com/cometbft/cometbft/scalaris/config"
	baseapp "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	svrcfg "github.com/cosmos/cosmos-sdk/server/config"
	svrtypes "github.com/cosmos/cosmos-sdk/server/types"
)

func tempQueryFn(req abci.RequestQuery) abci.ResponseQuery {
	println("tempQueryFn called")
	return abci.ResponseQuery{}
}

func StartExplorerApi(cfg *cfg.Config, logger log.Logger) error {
	logger.Info("Starting explorer API server...", "step_num", "1")
	cmd := server.StartCmd(nil, "/foobar")

	logger.Info("Starting explorer API server...", "step_num", "2")

	clientCtx, err := client.GetClientQueryContext(cmd)

	logger.Info("Starting explorer API server...", "step_num", "3")
	if err != nil {
		return err
	}

	rpcAddr := cfg.RPC.ListenAddress

	c, err := rpchttp.New(rpcAddr, "/websocket")
	logger.Info("Starting explorer API server...", "step_num", "4")
	if err != nil {
		panic(err)
	}

	clientCtx = clientCtx.WithClient(c)

	logger.Info("Starting explorer API server...", "step_num", "5")

	queryRouter := baseapp.NewGRPCQueryRouter()

	logger.Info("Starting explorer API server...", "step_num", "6")

	tmservice.RegisterTendermintService(
		clientCtx,
		queryRouter,
		types.NewInterfaceRegistry(),
		tempQueryFn,
	)

	logger.Info("Starting explorer API server...", "step_num", "7")

	apiServer := api.New(clientCtx, logger)

	logger.Info("Starting explorer API server...", "step_num", "8")
	tmservice.RegisterGRPCGatewayRoutes(clientCtx, apiServer.GRPCGatewayRouter)

	logger.Info("Starting explorer API server...", "step_num", "9")

	errCh := make(chan error)

	apiServerCfg := svrcfg.DefaultConfig()

	logger.Info("Starting explorer API server...", "step_num", "10")

	apiServerCfg.API.Enable = true
	apiServerCfg.API.Swagger = true
	apiServerCfg.API.Address = "0.0.0.0:1317"

	go func() {
		logger.Info("Starting API server...")
		if err := apiServer.Start(svrcfg.Config{
			API: svrcfg.APIConfig{
				Enable: true,
			},
		}); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err

	case <-time.After(svrtypes.ServerStartTime): // assume server started successfully
	}

	return nil
}
