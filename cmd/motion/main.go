package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/filecoin-project/motion"
	"github.com/ipfs/go-log/v2"
)

var logger = log.Logger("motion/cmd")

func main() {
	// TODO: add flags, options and all that jazz
	if _, set := os.LookupEnv("GOLOG_LOG_LEVEL"); !set {
		_ = log.SetLogLevel("*", "INFO")
	}
	m, err := motion.New()
	if err != nil {
		logger.Fatalw("Failed to instantiate Motion", "err", err)
	}
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		logger.Fatalw("Failed to start Motion", "err", err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	logger.Info("Terminating...")
	if err := m.Shutdown(ctx); err != nil {
		logger.Warnw("Failure occurred while shutting down Motion.", "err", err)
	} else {
		logger.Info("Shut down Motion successfully.")
	}
}
