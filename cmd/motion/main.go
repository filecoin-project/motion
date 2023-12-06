package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	singularityclient "github.com/data-preservation-programs/singularity/client/swagger/http"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/motion"
	"github.com/filecoin-project/motion/blob"
	"github.com/filecoin-project/motion/integration/singularity"
	"github.com/ipfs/go-log/v2"
	_ "github.com/joho/godotenv/autoload"
	"github.com/urfave/cli/v2"
)

var logger = log.Logger("motion/cmd")

func main() {
	if _, set := os.LookupEnv("GOLOG_LOG_LEVEL"); !set {
		_ = log.SetLogLevel("*", "INFO")
	}
	pwd, err := os.Getwd()
	if err != nil {
		logger.Fatal(err)
	}
	app := cli.App{
		Name:  "motion",
		Usage: "Propelling data onto Filecoin",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "storeDir",
				Usage:       "The path at which to store Motion data",
				DefaultText: "OS Temporary directory",
				Value:       os.TempDir(),
				EnvVars:     []string{"MOTION_STORE_DIR"},
			},
			&cli.StringFlag{
				Name:        "configDir",
				Usage:       "The path at which to store Motion config, e.g. sps.yml",
				DefaultText: "Current directory",
				Value:       pwd,
				EnvVars:     []string{"MOTION_CONFIG_DIR"},
			},
			&cli.Int64Flag{
				Name:        "minFreeDiskSpace",
				Usage:       "Minumum amount of free space that must remaio on disk after writing blob. Set -1 to disable checks.",
				DefaultText: "64 Mib",
				EnvVars:     []string{"MIN_FREE_DISK_SPACE"},
			},
			&cli.StringFlag{
				Name:    "walletKey",
				Usage:   "Hex encoded private key for the wallet to use with motion",
				EnvVars: []string{"MOTION_WALLET_KEY"},
			},
			&cli.BoolFlag{
				Name:        "experimentalSingularityStore",
				Usage:       "Whether to use experimental Singularity store as the storage and deal making engine",
				DefaultText: "Local storage is used",
				EnvVars:     []string{"MOTION_EXPERIMENTAL_SINGULARITY_STORE"},
			},
			&cli.StringFlag{
				Name:        "experimentalRemoteSingularityAPIUrl",
				Usage:       "When using a singularity as the storage engine, if set, uses a remote HTTP API to interface with Singularity",
				DefaultText: "use singularity as a code library",
				EnvVars:     []string{"MOTION_EXPERIMENTAL_REMOTE_SINGULARITY_API_URL"},
			},
			&cli.BoolFlag{
				Name:     "lotus-test",
				Category: "Lotus",
				EnvVars:  []string{"LOTUS_TEST"},
			},
			&cli.UintFlag{
				Name:        "replicationFactor",
				Usage:       "The number of desired replicas per blob",
				DefaultText: "Number of storage providers; see 'storageProvider' flag.",
			},
			&cli.StringFlag{
				Name:    "singularityMaxCarSize",
				Usage:   "The maximum Singularity generated CAR size",
				Value:   "31.5GiB",
				EnvVars: []string{"MOTION_SINGULARITY_MAX_CAR_SIZE"},
			},
			&cli.Int64Flag{
				Name:        "singularityPackThreshold",
				Usage:       "The Singularity store pack threshold in number of bytes",
				DefaultText: "17,179,869,184 (i.e. 16 GiB)",
				Value:       16 << 30,
				EnvVars:     []string{"MOTION_SINGULARITY_PACK_THRESHOLD"},
			},
			&cli.DurationFlag{
				Name:        "singularityForcePackAfter",
				Usage:       "The maximum amount of time to wait without any data being received before forcing packing",
				DefaultText: "24 hours",
				Value:       24 * time.Hour,
				EnvVars:     []string{"MOTION_SINGULARITY_FORCE_PACK_AFTER"},
			},
			&cli.DurationFlag{
				Name:    "experimentalSingularityCleanupInterval",
				Usage:   "How often to check for and delete files from the local store that have already had deals made",
				Value:   time.Hour,
				EnvVars: []string{"MOTION_SINGULARITY_LOCAL_CLEANUP_INTERVAL"},
			},
		},
		Action: func(cctx *cli.Context) error {
			if cctx.Bool("lotus-test") {
				logger.Info("Current network is set to Testnet")
				address.CurrentNetwork = address.Testnet
			} else {
				address.CurrentNetwork = address.Mainnet
			}
			storeDir := cctx.String("storeDir")
			configDir := cctx.String("configDir")
			var store blob.Store
			if cctx.Bool("experimentalSingularityStore") {
				singularityAPIUrl := cctx.String("experimentalRemoteSingularityAPIUrl")
				// Instantiate Singularity client depending on specified flags.
				var singClient *singularityclient.SingularityAPI
				if singularityAPIUrl != "" {
					singClient = singularityclient.NewHTTPClientWithConfig(
						nil,
						singularityclient.DefaultTransportConfig().WithHost(singularityAPIUrl),
					)
				} else {
					return fmt.Errorf("singularity API URL is required")
				}

				singularityStore, err := singularity.NewStore(
					singularity.WithStoreDir(storeDir),
					singularity.WithConfigDir(configDir),
					singularity.WithSingularityClient(singClient),
					singularity.WithWalletKey(cctx.String("walletKey")),
					singularity.WithMaxCarSize(cctx.String("singularityMaxCarSize")),
					singularity.WithPackThreshold(cctx.Int64("singularityPackThreshold")),
					singularity.WithForcePackAfter(cctx.Duration("singularityForcePackAfter")),
					singularity.WithCleanupInterval(cctx.Duration("experimentalSingularityCleanupInterval")),
					singularity.WithMinFreeSpace(cctx.Int64("minFreeDiskSpace")),
				)
				if err != nil {
					logger.Errorw("Failed to instantiate singularity store", "err", err)
					return err
				}
				logger.Infow("Using Singularity blob store", "storeDir", storeDir)
				if err := singularityStore.Start(cctx.Context); err != nil {
					logger.Errorw("Failed to start Singularity blob store", "err", err)
					return err
				}
				defer func() {
					if err := singularityStore.Shutdown(context.Background()); err != nil {
						logger.Errorw("Failed to shut down Singularity blob store", "err", err)
					}
				}()
				store = singularityStore
			} else {
				store = blob.NewLocalStore(storeDir, blob.WithMinFreeSpace(cctx.Int64("minFreeDiskSpace")))
				logger.Infow("Using local blob store", "storeDir", storeDir)
			}

			m, err := motion.New(motion.WithBlobStore(store))
			if err != nil {
				logger.Fatalw("Failed to instantiate Motion", "err", err)
			}
			ctx := cctx.Context

			if err := m.Start(ctx); err != nil {
				logger.Fatalw("Failed to start Motion", "err", err)
			}
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt)
			<-c
			logger.Info("Terminating...")
			if err := m.Shutdown(ctx); err != nil {
				logger.Warnw("Failure occurred while shutting down Motion.", "err", err)
			}
			logger.Info("Shut down Motion successfully.")
			return nil
		},
	}
	if err := app.Run(os.Args); err != nil {
		logger.Error(err)
	}
}

func durationToFilecoinEpoch(d time.Duration) abi.ChainEpoch {
	return abi.ChainEpoch(int64(d.Seconds()) / builtin.EpochDurationSeconds)
}

func attoFilToTokenAmount(v float64) abi.TokenAmount {
	return big.NewInt(int64(v * 1e18))
}
