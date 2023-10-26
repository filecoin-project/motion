package singularity

import (
	"bytes"
	"context"
	"errors"
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
	if r.offset >= r.size {
		return 0, io.EOF
	}

	// Figure out how many bytes to read
	readLen := int64(len(p))
	remainingBytes := r.size - r.offset
	if readLen > remainingBytes {
		readLen = remainingBytes
	}

	buf := bytes.NewBuffer(p)
	buf.Reset()

	n, err := r.writeToN(buf, readLen)
	return int(n), err
}

// WriteTo is implemented in order to directly handle io.Copy operations
// rather than allow small, separate Read operations.
func (r *SingularityReader) WriteTo(w io.Writer) (int64, error) {
	if r.offset >= r.size {
		return 0, io.EOF
	}
	// Read all remaining bytes and write them to w.
	return r.writeToN(w, r.size-r.offset)
}

func (r *SingularityReader) writeToN(w io.Writer, readLen int64) (int64, error) {
	_, _, err := r.client.File.RetrieveFile(&file.RetrieveFileParams{
		Context: context.Background(),
		ID:      int64(r.fileID),
		Range:   ptr.String(fmt.Sprintf("bytes=%d-%d", r.offset, r.offset+readLen-1)),
	}, w)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve file slice: %w", err)
	}

	r.offset += readLen

	return readLen, nil
}

func (r *SingularityReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.offset
	case io.SeekEnd:
		offset += r.size
	default:
		return 0, errors.New("unknown seek mode")
	}

	if offset > r.size {
		return 0, errors.New("seek past end of file")
	}
	if offset < 0 {
		return 0, errors.New("seek before start of file")
	}

	r.offset = offset

	return r.offset, nil
}

func (r *SingularityReader) Close() error {
	return nil
}
