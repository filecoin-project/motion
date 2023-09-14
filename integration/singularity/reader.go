package singularity

import (
	"bytes"
	"context"
	"fmt"
	"io"

	singularityclient "github.com/data-preservation-programs/singularity/client/swagger/http"
	"github.com/data-preservation-programs/singularity/client/swagger/http/file"
	"github.com/gotidy/ptr"
)

// io.ReadSeekCloser implementation that reads from remote singularity
type SingularityReader struct {
	client *singularityclient.SingularityAPI
	fileID uint64
	offset int64
	size   int64
}

func (r *SingularityReader) Read(p []byte) (int, error) {
	logger.Infof("buffer size: %v", len(p))

	buf := bytes.NewBuffer(p)
	buf.Reset()

	if r.offset >= r.size {
		return 0, io.EOF
	}

	// Figure out how many bytes to read
	readLen := int64(len(p))
	remainingBytes := r.size - r.offset
	if readLen > remainingBytes {
		readLen = remainingBytes
	}

	_, _, err := r.client.File.RetrieveFile(&file.RetrieveFileParams{
		Context: context.Background(),
		ID:      int64(r.fileID),
		Range:   ptr.String(fmt.Sprintf("bytes=%d-%d", r.offset, r.offset+readLen-1)),
	}, buf)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve file slice: %w", err)
	}

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
