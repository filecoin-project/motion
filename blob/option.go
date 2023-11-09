package blob

const (
	Kib = 1 << (10 * (iota + 1))
	Mib
	Gib
)

const defaultMinFreeSpace = 64 * Mib

// config contains all options for LocalStore.
type config struct {
	minFreeSpace uint64
}

// Option is a function that sets a value in a config.
type Option func(*config)

// getOpts creates a config and applies Options to it.
func getOpts(options []Option) config {
	cfg := config{
		minFreeSpace: defaultMinFreeSpace,
	}
	for _, opt := range options {
		opt(&cfg)
	}
	return cfg
}

// WithMinFreeSpace seta the minimum amount of free dist space that must remain
// after writing a blob. If unset or 0 then defaultMinFreeSpace is used. If -1, then
// no free space checks are performed.
func WithMinFreeSpace(space int64) Option {
	return func(c *config) {
		if space == 0 {
			space = defaultMinFreeSpace
		} else if space < 0 {
			space = 0
		}
		c.minFreeSpace = uint64(space)
	}
}
