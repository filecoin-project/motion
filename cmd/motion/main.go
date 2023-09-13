package main

import (
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
	"github.com/urfave/cli/v2"
)

var logger = log.Logger("motion/cmd")

func main() {
	if _, set := os.LookupEnv("GOLOG_LOG_LEVEL"); !set {
		_ = log.SetLogLevel("*", "INFO")
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
				Name:    "walletKey",
				Usage:   "Hex encoded private key for the wallet to use with motion",
				EnvVars: []string{"MOTION_WALLET_KEY"},
			},
			&cli.BoolFlag{
				Name:        "experimentalSingularityStore",
				Usage:       "Whether to use experimental Singularity store as the storage and deal making engine",
				DefaultText: "Local storage is used",
			},
			&cli.StringFlag{
				Name:        "experimentalRemoteSingularityAPIUrl",
				Usage:       "When using a singularity as the storage engine, if set, uses a remote HTTP API to interface with Singularity",
				DefaultText: "use singularity as a code library",
			},
			&cli.StringSliceFlag{
				Name:        "storageProvider",
				Aliases:     []string{"sp"},
				Usage:       "Storage providers to which to make deals with. Multiple providers may be specified.",
				DefaultText: "No deals are made to replicate data onto storage providers.",
				EnvVars:     []string{"MOTION_STORAGE_PROVIDERS"},
			},
			&cli.StringFlag{
				Name:     "lotusApi",
				Category: "Lotus",
				Usage:    "Lotus RPC API endpoint",
				Value:    "https://api.node.glif.io/rpc/v1",
				EnvVars:  []string{"LOTUS_API"},
			},
			&cli.StringFlag{
				Name:     "lotusToken",
				Category: "Lotus",
				Usage:    "Lotus RPC API token",
				Value:    "",
				EnvVars:  []string{"LOTUS_TOKEN"},
			},
			&cli.BoolFlag{
				Name:     "lotus-test",
				Category: "Lotus",
				EnvVars:  []string{"LOTUS_TEST"},
				Action: func(context *cli.Context, lotusTest bool) error {
					if lotusTest {
						logger.Info("Current network is set to Testnet")
						address.CurrentNetwork = address.Testnet
					}
					return nil
				},
			},
			&cli.UintFlag{
				Name:        "replicationFactor",
				Usage:       "The number of desired replicas per blob",
				DefaultText: "Number of storage providers; see 'storageProvider' flag.",
			},
			&cli.Float64Flag{
				Name:    "pricePerGiBEpoch",
				Usage:   "The maximum price per GiB per Epoch in attoFIL.",
				EnvVars: []string{"MOTION_PRICE_PER_GIB_EPOCH"},
			},
			&cli.Float64Flag{
				Name:    "pricePerGiB",
				Usage:   "The maximum  price per GiB in attoFIL.",
				EnvVars: []string{"MOTION_PRICE_PER_GIB"},
			},
			&cli.Float64Flag{
				Name:    "pricePerDeal",
				Usage:   "The maximum price per deal in attoFIL.",
				EnvVars: []string{"MOTION_PRICE_PER_DEAL"},
			},
			&cli.DurationFlag{
				Name:        "dealStartDelay",
				Usage:       "The deal start epoch delay.",
				DefaultText: "72 hours",
				Value:       72 * time.Hour,
				EnvVars:     []string{"MOTION_DEAL_START_DELAY"},
			},
			&cli.DurationFlag{
				Name:        "dealDuration",
				Usage:       "The duration of deals made on Filecoin",
				DefaultText: "One year (356 days)",
				Value:       356 * 24 * time.Hour,
				EnvVars:     []string{"MOTION_DEAL_DURATION"},
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
			&cli.BoolFlag{
				Name:        "verifiedDeal",
				Usage:       "whether deals made with motion should be verified deals",
				DefaultText: "Deals are verified",
				Value:       false,
				EnvVars:     []string{"MOTION_VERIFIED_DEAL"},
			},
			&cli.StringFlag{
				Name:        "experimentalSingularityContentURLTemplate",
				Usage:       "When using a singularity as the storage engine, if set, setups up online deals to use the given url template for making online deals",
				DefaultText: "make offline deals",
			},
			&cli.StringFlag{
				Name:        "experimentalSingularityScheduleCron",
				Usage:       "When using a singularity as the storage engine, if set, setups up the cron schedule to send out batch deals.",
				DefaultText: "runs every minute",
				Value:       "* * * * *",
				EnvVars:     []string{"MOTION_SINGULARITY_SCHEDULE_CRON"},
			},
			&cli.IntFlag{
				Name:        "experimentalSingularityScheduleDealNumber",
				Usage:       "When using a singularity as the storage engine, if set, setups up the max deal number per triggered schedule.",
				DefaultText: "1 per trigger",
				Value:       1,
				EnvVars:     []string{"MOTION_SINGULARITY_SCHEDULE_DEAL_NUMBER"},
			},
		},
		Action: func(cctx *cli.Context) error {
			storeDir := cctx.String("storeDir")
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

				// Parse any configured storage provider addresses.
				sps := cctx.StringSlice("storageProvider")
				spAddrs := make([]address.Address, 0, len(sps))
				for _, sp := range sps {
					spAddr, err := address.NewFromString(sp)
					if err != nil {
						return fmt.Errorf("storage provider '%s' is not a valid address: %w", sp, err)
					}
					spAddrs = append(spAddrs, spAddr)
				}
				singularityStore, err := singularity.NewStore(
					singularity.WithStoreDir(cctx.String("storeDir")),
					singularity.WithStorageProviders(spAddrs...),
					singularity.WithReplicationFactor(cctx.Uint("replicationFactor")),
					singularity.WithPricePerGiBEpoch(attoFilToTokenAmount(cctx.Float64("pricePerGiBEpoch"))),
					singularity.WithPricePerGiB(attoFilToTokenAmount(cctx.Float64("pricePerGiB"))),
					singularity.WithPricePerDeal(attoFilToTokenAmount(cctx.Float64("pricePerDeal"))),
					singularity.WithDealStartDelay(durationToFilecoinEpoch(cctx.Duration("dealStartDelay"))),
					singularity.WithDealDuration(durationToFilecoinEpoch(cctx.Duration("dealDuration"))),
					singularity.WithSingularityClient(singClient),
					singularity.WithWalletKey(cctx.String("walletKey")),
					singularity.WithMaxCarSize(cctx.String("singularityMaxCarSize")),
					singularity.WithPackThreshold(cctx.Int64("singularityPackThreshold")),
					singularity.WithScheduleUrlTemplate(cctx.String("experimentalSingularityContentURLTemplate")),
					singularity.WithScheduleCron(cctx.String("experimentalSingularityScheduleCron")),
					singularity.WithScheduleDealNumber(cctx.Int("experimentalSingularityScheduleDealNumber")),
					singularity.WithVerifiedDeal(cctx.Bool("verifiedDeal")),
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
				store = singularityStore
			} else {
				store = blob.NewLocalStore(storeDir)
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
