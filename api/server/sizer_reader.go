package server

import "io"

var _ interface{ Size() int64 } = (*sizerReadCloser)(nil)

type sizerReadCloser struct {
	io.ReadCloser
	size int64
}

func (s sizerReadCloser) Size() int64 {
	return s.size
}
