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
	queryRouter := baseapp.NewGRPCQueryRouter()
	interfaceRegistry := types.NewInterfaceRegistry()
	queryRouter.SetInterfaceRegistry(interfaceRegistry)

	logger.Info("Starting explorer API server...", "step_num", "2", "query_router", queryRouter, "interface_registry", interfaceRegistry)

	clientCtx := client.Context{}.WithInterfaceRegistry(interfaceRegistry)

	rpcAddr := cfg.RPC.ListenAddress

	c, err := rpchttp.New(rpcAddr, "/websocket")
	c.Logger = logger

	logger.Info("Starting explorer API server...", "step_num", "4")
	if err != nil {
		panic(err)
	}

	clientCtx = clientCtx.WithClient(c)

	logger.Info("Starting explorer API server...", "step_num", "5")

	logger.Info("Starting explorer API server...", "step_num", "6")

	tmservice.RegisterTendermintService(
		clientCtx,
		queryRouter,
		interfaceRegistry,
		tempQueryFn,
	)

	logger.Info("Starting explorer API server...", "step_num", "7")

	apiServer := api.New(clientCtx, logger)

	logger.Info("Starting explorer API server...", "step_num", "8")
	tmservice.RegisterGRPCGatewayRoutes(apiServer.ClientCtx, apiServer.GRPCGatewayRouter)

	logger.Info("Starting explorer API server...", "step_num", "9")

	errCh := make(chan error)

	logger.Info("Starting explorer API server...", "step_num", "10")

	apiServerCfg := svrcfg.DefaultConfig()
	apiServerCfg.API.Enable = true
	apiServerCfg.API.EnableUnsafeCORS = true
	apiServerCfg.API.Address = "tcp://0.0.0.0:1317"

	logger.Info("Starting explorer API server...", "step_num", "11")

	go func() {
		logger.Info("Starting API server...", "config", *apiServerCfg)
		if err := apiServer.Start(*apiServerCfg); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err

	case <-time.After(svrtypes.ServerStartTime): // assume server started successfully
	}

	logger.Info("Explorer API server started successfully", "routes", apiServer.Router, "interface_registry", interfaceRegistry, "query_router", queryRouter)

	return nil
}
