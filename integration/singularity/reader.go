package singularity

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/data-preservation-programs/singularity/client/swagger/http/file"
	"github.com/gotidy/ptr"
)

// io.ReadSeekCloser implementation that reads from remote singularity
type SingularityReader struct {
	store  *SingularityStore
	fileID uint64
	offset int64
	size   int64
}

func (r *SingularityReader) Read(p []byte) (int, error) {
	buf := bytes.NewBuffer(p)
	buf.Reset()
	writer := &syncWriter{
		inner:      buf,
		offset:     0,
		targetSize: int64(len(p)),
		done:       make(chan struct{}, 1),
	}

	if r.offset >= r.size {
		return 0, io.EOF
	}

	// Figure out how many bytes to read
	readLen := int64(len(p))
	remainingBytes := r.size - r.offset
	if readLen > remainingBytes {
		readLen = remainingBytes
	}

	_, _, err := r.store.singularityClient.File.RetrieveFile(&file.RetrieveFileParams{
		Context: context.Background(),
		ID:      int64(r.fileID),
		Range:   ptr.String(fmt.Sprintf("bytes=%d-%d", r.offset, readLen)),
	}, writer)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve file slice: %w", err)
	}

	<-writer.done

	r.offset += readLen

	return int(readLen), nil
}

func (r *SingularityReader) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64

	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = r.offset + offset
	case io.SeekEnd:
		newOffset = r.size + offset
	}

	if newOffset > r.size {
		return 0, fmt.Errorf("byte offset would exceed file size")
	}

	r.offset = newOffset

	return r.offset, nil
}

func (r *SingularityReader) Close() error {
	return nil
}

// Sends a signal when done writing a target amount of bytes
type syncWriter struct {
	inner      io.Writer
	offset     int64
	targetSize int64
	// Must have buffer size of 1
	done chan struct{}
}

func (w *syncWriter) Write(p []byte) (int, error) {
	// Move offset forward and signal done if it hits or exceeds targetSize
	w.offset += int64(len(p))
	if w.offset >= w.targetSize {
		select {
		case w.done <- struct{}{}:
		default:
		}
	}

	return w.inner.Write(p)
}
