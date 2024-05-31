package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/cometbft/cometbft/cmd/cometbft/commands"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/cli"
	cmtflags "github.com/cometbft/cometbft/libs/cli/flags"
	"github.com/cometbft/cometbft/libs/log"
	cfg "github.com/cometbft/cometbft/scalaris/config"
)

var (
	config = cfg.DefaultConfig()
	logger = log.NewTMLogger(log.NewSyncWriter(os.Stdout))
)

func init() {
	registerFlagsRootCmd(RootCmd)
}

func registerFlagsRootCmd(cmd *cobra.Command) {
	cmd.PersistentFlags().String("log_level", config.LogLevel, "log level")
}

// ParseConfig retrieves the default environment configuration,
// sets up the CometBFT root and ensures that the root exists
func ParseConfig(cmd *cobra.Command) (*cfg.Config, error) {
	conf := cfg.DefaultConfig()
	err := viper.Unmarshal(conf)
	if err != nil {
		return nil, err
	}

	var home string
	if os.Getenv("CMTHOME") != "" {
		home = os.Getenv("CMTHOME")
	} else if os.Getenv("TMHOME") != "" {
		// XXX: Deprecated.
		home = os.Getenv("TMHOME")
		logger.Error("Deprecated environment variable TMHOME identified. CMTHOME should be used instead.")
	} else {
		home, err = cmd.Flags().GetString(cli.HomeFlag)
		if err != nil {
			return nil, err
		}
	}

	conf.RootDir = home

	conf.SetRoot(conf.RootDir)
	cmtcfg.EnsureRoot(conf.RootDir)
	if err := conf.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("error in config file: %v", err)
	}
	if warnings := conf.CheckDeprecated(); len(warnings) > 0 {
		for _, warning := range warnings {
			logger.Info("deprecated usage found in configuration file", "usage", warning)
		}
	}
	return conf, nil
}

// RootCmd is the root command for CometBFT core.
var RootCmd = &cobra.Command{
	Use:   "scalaris",
	Short: "Adapter for scalaris consesnsus framework",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		if cmd.Name() == commands.VersionCmd.Name() {
			return nil
		}

		config, err = ParseConfig(cmd)
		if err != nil {
			return err
		}

		if config.LogFormat == cmtcfg.LogFormatJSON {
			logger = log.NewTMJSONLogger(log.NewSyncWriter(os.Stdout))
		}

		logger, err = cmtflags.ParseLogLevel(config.LogLevel, logger, cmtcfg.DefaultLogLevel)
		if err != nil {
			return err
		}

		if viper.GetBool(cli.TraceFlag) {
			logger = log.NewTracingLogger(logger)
		}

		logger = logger.With("module", "main")
		return nil
	},
}
