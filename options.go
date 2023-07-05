package motion

import (
	"os"

	"github.com/filecoin-project/motion/api/server"
	"github.com/filecoin-project/motion/blob"
)

type (
	Option  func(*options) error
	options struct {
		serverOptions []server.Option
		blobStore     blob.Store
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, err
		}
	}
	if opts.blobStore == nil {
		dir := os.TempDir()
		logger.Warnw("No blob store is specified. Falling back on local blob store in temporary directory.", "dir", dir)
		opts.blobStore = blob.NewLocalStore(dir)
	}
	return opts, nil
}

func WithServerOptions(serverOptions ...server.Option) Option {
	return func(o *options) error {
		o.serverOptions = serverOptions
		return nil
	}
}

func WithBlobStore(s blob.Store) Option {
	return func(o *options) error {
		o.blobStore = s
		return nil
	}
}
