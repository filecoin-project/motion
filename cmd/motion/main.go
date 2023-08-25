package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"

	"github.com/data-preservation-programs/singularity/client"
	httpclient "github.com/data-preservation-programs/singularity/client/http"
	libclient "github.com/data-preservation-programs/singularity/client/lib"
	"github.com/data-preservation-programs/singularity/database"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/motion"
	"github.com/filecoin-project/motion/blob"
	"github.com/filecoin-project/motion/wallet"
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
			&cli.BoolFlag{
				Name:        "experimentalRibsStore",
				Usage:       "Whether to use experimental RIBS as the storage and deal making",
				DefaultText: "Local storage is used",
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
				Usage:       "whether to use experimental Singularity store as the storage and deal making engine",
				DefaultText: "Local storage is used",
			},
			&cli.StringFlag{
				Name:        "experimentalRemoteSingularityAPIUrl",
				Usage:       "when using a singularity as the storage engine, if set, uses a remote HTTP API to interface with Singularity",
				DefaultText: "use singularity as a code library",
			},
			&cli.StringSliceFlag{
				Name:        "storageProvider",
				Aliases:     []string{"sp"},
				Usage:       "Storage providers to which to make deals with. Multiple providers may be specified.",
				DefaultText: "No deals are made to replicate data onto storage providers.",
			},
		},
		Action: func(cctx *cli.Context) error {

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

			storeDir := cctx.String("storeDir")
			var store blob.Store
			if cctx.Bool("experimentalRibsStore") {
				rbstore, err := blob.NewRibsStore(storeDir, ks)
				if err != nil {
					return err
				}
				logger.Infow("Using RIBS blob store", "storeDir", storeDir)
				if err := rbstore.Start(cctx.Context); err != nil {
					logger.Errorw("Failed to start RIBS blob store", "err", err)
					return err
				}
				store = rbstore
			} else if cctx.Bool("experimentalSingularityStore") {
				singularityAPIUrl := cctx.String("experimentalRemoteSingularityAPIUrl")
				var client client.Client
				if singularityAPIUrl != "" {
					client = httpclient.NewHTTPClient(http.DefaultClient, singularityAPIUrl)
				} else {
					db, err := database.OpenWithDefaults("sqlite:" + storeDir + "/singularity.db")
					if err != nil {
						logger.Errorw("Failed to open singularity database", "err", err)
						return err
					}
					client, err = libclient.NewClient(db)
					if err != nil {
						logger.Errorw("Failed to get singularity client", "err", err)
						return err
					}
				}
				singularityStore := blob.NewSingularityStore(storeDir, client)
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
			m, err := motion.New(
				motion.WithBlobStore(store),
				motion.WithWallet(wallet),
				motion.WithStorageProviders(spAddrs...),
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
			// TODO: Re-enable once the panic is fixed. See: https://github.com/FILCAT/ribs/issues/39
			//if rbstore, ok := store.(*blob.RibsStore); ok {
			//	if err := rbstore.Shutdown(ctx); err != nil {
			//		logger.Warnw("Failure occurred while shutting down RIBS blob store.", "err", err)
			//	}
			//}
			logger.Info("Shut down Motion successfully.")
			return nil
		},
	}
	if err := app.Run(os.Args); err != nil {
		logger.Error(err)
	}
}
