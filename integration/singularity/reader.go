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

	// Reads remaining data from current range.
	rangeReader *rangeReader
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

	n, err := r.WriteToN(buf, readLen)
	return int(n), err
}

// WriteTo is implemented in order to directly handle io.Copy operations
// rather than allow small, separate Read operations.
func (r *SingularityReader) WriteTo(w io.Writer) (int64, error) {
	if r.offset >= r.size {
		return 0, io.EOF
	}
	// Read all remaining bytes and write them to w.
	return r.WriteToN(w, r.size-r.offset)
}

func (r *SingularityReader) WriteToN(w io.Writer, readLen int64) (int64, error) {
	var read int64
	// If there is a rangeReader from the previous read that can be used to
	// continue reading more data, then use it instead of doing another
	// findFileRanges and Retrieve for more reads from this same range.
	if r.rangeReader != nil {
		// If continuing from the previous read, keep reading from this rangeReader.
		if r.offset == r.rangeReader.offset {
			// Reading data leftover from previous read into w.
			n, err := r.rangeReader.writeToN(w, readLen)
			if err != nil && !errors.Is(err, io.EOF) {
				return 0, err
			}
			r.offset += n
			readLen -= n
			read += n
			if readLen == 0 {
				// Read all requested data from leftover in rangeReader.
				return read, nil
			}
			// No more leftover data to read, but readLen additional bytes
			// still needed. Will read more data from next range(s).
		}
		// No more leftover data in rangeReader, or seek to done since last read.
		r.rangeReader.close()
		r.rangeReader = nil
	}

	rangeReadLen := readLen
	offsetInRange := r.offset - r.size
	remainingRange := r.size - offsetInRange
	if rangeReadLen > remainingRange {
		rangeReadLen = remainingRange
	}

	byteRange := fmt.Sprintf("bytes=%d-%d", r.offset, r.offset+readLen-1)
	rr := &rangeReader{
		offset:    r.offset,
		reader:    r.retrieveReader(context.Background(), int64(r.fileID), byteRange),
		remaining: remainingRange,
	}

	// Reading readLen of the remaining bytes in this range.
	n, err := rr.writeToN(w, readLen)
	if err != nil && !errors.Is(err, io.EOF) {
		rr.close()
		return 0, err
	}
	r.offset += n
	readLen -= n
	read += n

	// check for missing file ranges at the end
	if readLen > 0 {
		rr.close()
		return read, fmt.Errorf("not enough data to serve entire range %s", byteRange)
	}

	// Some unread data left over in this range. Save it for next read.
	if rr.remaining != 0 {
		// Saving leftover rangeReader with rr.remaining bytes left.
		r.rangeReader = rr
	} else {
		// Leftover rangeReader has 0 bytes remaining.
		rr.close()
	}

	return read, nil
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
	var err error
	if r.rangeReader != nil {
		err = r.rangeReader.close()
		r.rangeReader = nil
	}
	return err
}

func (r *SingularityReader) retrieveReader(ctx context.Context, fileID int64, byteRange string) io.ReadCloser {
	// Start goroutine to read from singularity into write end of pipe.
	reader, writer := io.Pipe()
	go func() {
		_, _, err := r.client.File.RetrieveFile(&file.RetrieveFileParams{
			Context: ctx,
			ID:      fileID,
			Range:   ptr.String(byteRange),
		}, writer)
		if err != nil {
			err = fmt.Errorf("failed to retrieve file slice: %w", err)
		}
		writer.CloseWithError(err)
	}()

	// Return the read end of pipe.
	return reader
}
