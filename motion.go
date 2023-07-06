package motion

import (
	"context"

	"github.com/filecoin-project/motion/api/server"
	"github.com/ipfs/go-log/v2"
)

var (
	logger = log.Logger("motion")
)

// Motion is a service that exposes a simple HTTP API to create / retrieve blobs of data to and from FileCoin Network.
type Motion struct {
	*options
	httpServer *server.HttpServer
}

// New Instantiates a new Motion service.
func New(o ...Option) (*Motion, error) {
	opts, err := newOptions(o...)
	if err != nil {
		return nil, err
	}
	httpServer, err := server.NewHttpServer(opts.blobStore)
	if err != nil {
		return nil, err
	}
	return &Motion{
		options:    opts,
		httpServer: httpServer,
	}, nil
}

// Start starts the motion services.
func (m *Motion) Start(ctx context.Context) error {
	// TODO start other components like deal engine, wallets etc.
	return m.httpServer.Start(ctx)
}

// Shutdown Start shuts down the motion services.
func (m *Motion) Shutdown(ctx context.Context) error {
	return m.httpServer.Shutdown(ctx)
}
