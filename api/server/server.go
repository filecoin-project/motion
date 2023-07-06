package server

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/filecoin-project/motion/blob"
	"github.com/ipfs/go-log/v2"
)

var logger = log.Logger("motion/api/server")

// HttpServer is Motion API the HTTP server.
type HttpServer struct {
	*options
	httpServer *http.Server
	store      blob.Store
}

// NewHttpServer instantiates a new HTTP server that stores and retrieves blobs via the given store.
// See Option.
func NewHttpServer(store blob.Store, o ...Option) (*HttpServer, error) {
	opts, err := newOptions(o...)
	if err != nil {
		return nil, err
	}
	server := &HttpServer{
		options: opts,
		store:   store,
	}
	server.httpServer = &http.Server{
		Handler: server.ServeMux(),
	}
	return server, nil
}

// Start Starts the HTTP server.
// See Shutdown.
func (m *HttpServer) Start(_ context.Context) error {
	listener, err := net.Listen("tcp", m.httpListenAddr)
	if err != nil {
		return err
	}
	go func() {
		if err := m.httpServer.Serve(listener); errors.Is(err, http.ErrServerClosed) {
			logger.Info("HTTP server stopped successfully.")
		} else {
			logger.Errorw("HTTP server stopped erroneously.", "err", err)
		}
	}()
	logger.Infow("HTTP server started successfully.", "address", listener.Addr())
	return nil
}

// ServeMux returns a new HTTP handler for the endpoints supported by the server.
func (m *HttpServer) ServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/blob", m.handleBlobRoot)
	mux.HandleFunc("/v0/blob/", m.handleBlobSubtree)
	mux.HandleFunc("/", m.handleRoot)
	return mux
}

// Shutdown shuts down the HTTP Server.
func (m *HttpServer) Shutdown(ctx context.Context) error {
	return m.httpServer.Shutdown(ctx)
}
