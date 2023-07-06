package server

type (
	// Option is a configurable parameter in HttpServer.
	Option  func(*options) error
	options struct {
		httpListenAddr string
		maxBlobLength  uint64
	}
)

func newOptions(o ...Option) (*options, error) {
	opts := &options{
		httpListenAddr: "0.0.0.0:40080",
		maxBlobLength:  32 << 30, // 32 GiB
	}
	for _, apply := range o {
		if err := apply(opts); err != nil {
			return nil, err
		}
	}
	return opts, nil
}

// WithHttpListenAddr sets the HTTP server listen address.
// Defaults to 0.0.0.0:40080 if unspecified.
func WithHttpListenAddr(addr string) Option {
	return func(o *options) error {
		o.httpListenAddr = addr
		return nil
	}
}

// WithMaxBlobLength sets the maximum blob length accepted by the HTTP blob upload API.
// Defaults to 32 GiB.
func WithMaxBlobLength(l uint64) Option {
	return func(o *options) error {
		o.maxBlobLength = l
		return nil
	}
}
