package server

type (
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

func WithHttpListenAddr(addr string) Option {
	return func(o *options) error {
		o.httpListenAddr = addr
		return nil
	}
}

func WithMaxBlobLength(l uint64) Option {
	return func(o *options) error {
		o.maxBlobLength = l
		return nil
	}
}
