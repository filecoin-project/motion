package main

import (
	"os"
	"os/signal"

	"github.com/filecoin-project/motion"
	"github.com/filecoin-project/motion/blob"
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
		Usage: "Propelling data onto FileCoin",
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
		},
		Action: func(cctx *cli.Context) error {
			storeDir := cctx.String("storeDir")
			var store blob.Store
			if cctx.Bool("experimentalRibsStore") {
				rbstore, err := blob.NewRibsStore(storeDir)
				if err != nil {
					return err
				}
				logger.Infow("Using RIBS blob store", "storeDir", storeDir)
				if err := rbstore.Start(cctx.Context); err != nil {
					logger.Errorw("Failed to start RIBS blob store", "err", err)
					return err
				}
				store = rbstore
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
