package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/data-preservation-programs/singularity/client"
	httpclient "github.com/data-preservation-programs/singularity/client/http"
	libclient "github.com/data-preservation-programs/singularity/client/lib"
	"github.com/data-preservation-programs/singularity/database"
	"github.com/data-preservation-programs/singularity/service/epochutil"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/motion"
	"github.com/filecoin-project/motion/blob"
	"github.com/filecoin-project/motion/replicationconfig"
	"github.com/filecoin-project/motion/wallet"
	"github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"github.com/ybbus/jsonrpc/v3"
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
				Name:        "localWalletDir",
				Usage:       "The path to the local wallet directory.",
				DefaultText: "Defaults to '<user-home-directory>/.motion/wallet' with wallet key auto-generated if not present. Note that the directory permissions must be at most 0600.",
				EnvVars:     []string{"MOTION_LOCAL_WALLET_DIR"},
			},
			&cli.BoolFlag{
				Name:  "localWalletGenerateIfNotExist",
				Usage: "Whether to generate the local wallet key if none is found",
				Value: true,
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
			},
			&cli.UintFlag{
				Name:        "replicationFactor",
				Usage:       "The number of desired replicas per blob",
				DefaultText: "Number of storage providers; see 'storageProvider' flag.",
			},
			&cli.Float64Flag{
				Name:  "pricePerGiBEpoch",
				Usage: "The maximum price per GiB per Epoch in attoFIL.",
			},
			&cli.Float64Flag{
				Name:  "pricePerGiB",
				Usage: "The maximum  price per GiB in attoFIL.",
			},
			&cli.Float64Flag{
				Name:  "pricePerDeal",
				Usage: "The maximum price per deal in attoFIL.",
			},
			&cli.DurationFlag{
				Name:        "dealStartDelay",
				Usage:       "The deal start epoch delay.",
				DefaultText: "72 hours",
				Value:       72 * time.Hour,
			},
			&cli.DurationFlag{
				Name:        "dealDuration",
				Usage:       "The duration of deals made on Filecoin",
				DefaultText: "One year (356 days)",
				Value:       356 * 24 * time.Hour,
			},
		},
		Action: func(cctx *cli.Context) error {
			lotusTest := cctx.Bool("lotus-test")
			if lotusTest {
				address.CurrentNetwork = address.Testnet
			}

			localWalletDir := cctx.String("localWalletDir")
			localWalletGenIfNotExist := cctx.Bool("localWalletGenerateIfNotExist")
			ks, err := wallet.DefaultDiskKeyStoreOpener(localWalletDir, localWalletGenIfNotExist)()
			if err != nil {
				logger.Errorw("Failed to instantiate local wallet keystore", "err", err)
				return err
			}
			wallet, err := wallet.New(
				wallet.WithKeyStoreOpener(func() (types.KeyStore, error) { return ks, nil }),
				wallet.WithGenerateKeyIfNotExist(localWalletGenIfNotExist))
			if err != nil {
				logger.Errorw("Failed to instantiate local wallet", "err", err)
				return err
			}
			// Parse any configured storage povider addresses.
			sps := cctx.StringSlice("storageProvider")
			spAddrs := make([]address.Address, 0, len(sps))
			for _, sp := range sps {
				spAddr, err := address.NewFromString(sp)
				if err != nil {
					return fmt.Errorf("storage provider '%s' is not a valid address: %w", sp, err)
				}
				spAddrs = append(spAddrs, spAddr)
			}
			replicationConfig, err := replicationconfig.NewReplicationConfig(
				replicationconfig.WithStorageProviders(spAddrs...),
				replicationconfig.WithReplicationFactor(cctx.Uint("replicationFactor")),
				replicationconfig.WithPricePerGiBEpoch(attoFilToTokenAmount(cctx.Float64("pricePerGiBEpoch"))),
				replicationconfig.WithPricePerGiB(attoFilToTokenAmount(cctx.Float64("pricePerGiB"))),
				replicationconfig.WithPricePerDeal(attoFilToTokenAmount(cctx.Float64("pricePerDeal"))),
				replicationconfig.WithDealStartDelay(durationToFilecoinEpoch(cctx.Duration("dealStartDelay"))),
				replicationconfig.WithDealDuration(durationToFilecoinEpoch(cctx.Duration("dealDuration"))),
			)
			if err != nil {
				return fmt.Errorf("error initializing replication config: %w", err)
			}
			storeDir := cctx.String("storeDir")
			var store blob.Store
			lotusAPI := cctx.String("lotusApi")
			lotusToken := cctx.String("lotusToken")
			if cctx.Bool("experimentalSingularityStore") {
				singularityAPIUrl := cctx.String("experimentalRemoteSingularityAPIUrl")
				var client client.Client
				if singularityAPIUrl != "" {
					client = httpclient.NewHTTPClient(http.DefaultClient, singularityAPIUrl)
				} else {
					db, closer, err := database.OpenWithLogger("sqlite:" + storeDir + "/singularity.db")
					defer closer.Close()
					if err != nil {
						logger.Errorw("Failed to open singularity database", "err", err)
						return err
					}
					err = epochutil.Initialize(cctx.Context, lotusAPI, lotusToken)
					if err != nil {
						logger.Errorw("Failed to initialized epoch timing", "err", err)
						return err
					}
					client, err = libclient.NewClient(db, jsonrpc.NewClient(lotusAPI))
					if err != nil {
						logger.Errorw("Failed to get singularity client", "err", err)
						return err
					}
				}
				singularityStore := blob.NewSingularityStore(storeDir, wallet, replicationConfig, client)
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

			m, err := motion.New(
				motion.WithBlobStore(store),
				motion.WithWallet(wallet),
				motion.WithReplicationConfig(replicationConfig),
			)
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
