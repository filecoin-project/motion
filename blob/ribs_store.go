package blob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/boxo/chunker"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/lotus-web3/ribs"
	"github.com/lotus-web3/ribs/rbdeal"
	"github.com/multiformats/go-multihash"
)

// TODO parameterize this.
const ribsStoreChunkSize = 1 << 20 // 1 MiB

var (
	_ Store             = (*RibsStore)(nil)
	_ io.ReadSeekCloser = (*ribsStoredBlobReader)(nil)
)

type (
	// RibsStore is an experimental Store implementation that uses RIBS.
	// See: https://github.com/filcat/ribs
	RibsStore struct {
		ribs     ribs.RIBS
		maxSize  int
		indexDir string
	}
	ribsStoredBlob struct {
		*Descriptor
		Chunks []cid.Cid `json:"chunks"`
	}
	ribsStoredBlobReader struct {
		sess   ribs.Session
		blob   *ribsStoredBlob
		offset int64

		currentChunkIndex       int
		currentChunkReader      *bytes.Reader
		currentChunkPendingSeek int64
	}
)

// NewRibsStore instantiates a new experimental RIBS store.
func NewRibsStore(dir string) (*RibsStore, error) {
	rbdealDir := path.Join(path.Clean(dir), "rbdeal")
	if err := os.Mkdir(rbdealDir, 0750); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("failed to create internal directories: %w", err)
	}
	indexDir := path.Join(path.Clean(dir), "index")
	if err := os.Mkdir(indexDir, 0750); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("failed to create internal directories: %w", err)
	}

	// TODO Path to wallet is hardcoded in RIBS. Parameterise it and allow user to configure
	//      See: https://github.com/FILCAT/ribs/blob/7c8766206ec1e5ec30c613cde2b3a49d0ccf25d0/rbdeal/ribs.go#L156

	rbs, err := rbdeal.Open(rbdealDir)
	if err != nil {
		return nil, err
	}
	return &RibsStore{
		ribs:     rbs,
		maxSize:  32 << 30, // 32 GiB
		indexDir: indexDir,
	}, nil

}

func (r *RibsStore) Start(_ context.Context) error {
	// TODO: change RIBS to take context.
	return r.ribs.Start()
}
func (r *RibsStore) Put(ctx context.Context, in io.ReadCloser) (*Descriptor, error) {

	// Generate ID early to fail early if generation fails.
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	modtime := time.Now().UTC()

	// TODO incorporate https://github.com/filecoin-project/data-prep-tools/tree/main/docs/best-practices
	//      also see: https://github.com/anjor/anelace
	// TODO we could do things here to make commp etc. more efficient.
	//      for now this implementation remains highly experimental and optimised for velocity.
	batch := r.ribs.Session(ctx).Batch(ctx)

	splitter := chunk.NewSizeSplitter(in, ribsStoreChunkSize)

	// TODO: Store the byte ranges for satisfying io.ReadSeaker in case chunk size is not constant across blocks?
	var chunkCids []cid.Cid
	var size int
SplitLoop:
	for {
		b, err := splitter.NextBytes()
		switch err {
		case io.EOF:
			break SplitLoop
		case nil:
			size += len(b)
			if size > r.maxSize {
				return nil, ErrBlobTooLarge
			}
			mh, err := multihash.Sum(b, multihash.SHA2_256, -1)
			if err != nil {
				return nil, err
			}
			blk, err := blocks.NewBlockWithCid(b, cid.NewCidV1(cid.Raw, mh))
			if err != nil {
				return nil, err
			}
			if err := batch.Put(ctx, []blocks.Block{blk}); err != nil {
				return nil, err
			}
			chunkCids = append(chunkCids, blk.Cid())
		default:
			return nil, err
		}
	}
	if err := batch.Flush(ctx); err != nil {
		return nil, err
	}
	storedBlob := &ribsStoredBlob{
		Descriptor: &Descriptor{
			ID:               ID(id),
			Size:             uint64(size),
			ModificationTime: modtime,
		},
		Chunks: chunkCids,
	}
	index, err := os.Create(path.Join(r.indexDir, id.String()))
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(index).Encode(storedBlob); err != nil {
		return nil, err
	}
	return storedBlob.Descriptor, nil
}

func (r *RibsStore) Get(ctx context.Context, id ID) (io.ReadSeekCloser, error) {
	storedBlob, err := r.describeRibsStoredBlob(ctx, id)
	if err != nil {
		return nil, err
	}
	session := r.ribs.Session(ctx)
	reader, err := newRibsStoredBlobReader(session, storedBlob)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func (r *RibsStore) Describe(ctx context.Context, id ID) (*Descriptor, error) {
	storedBlob, err := r.describeRibsStoredBlob(ctx, id)
	if err != nil {
		return nil, err
	}
	return storedBlob.Descriptor, err
}

func (r *RibsStore) describeRibsStoredBlob(_ context.Context, id ID) (*ribsStoredBlob, error) {
	switch index, err := os.Open(path.Join(r.indexDir, id.String())); {
	case err == nil:
		var storedBlob ribsStoredBlob
		err := json.NewDecoder(index).Decode(&storedBlob)
		// TODO: populate descriptor status with FileCoin chain data about the stored blob.
		return &storedBlob, err
	case errors.Is(err, os.ErrNotExist):
		return nil, ErrBlobNotFound
	default:
		return nil, err
	}
}

func (r *RibsStore) Shutdown(_ context.Context) error {
	// TODO: change RIBS to take context.
	return r.ribs.Close()
}

func (rsb *ribsStoredBlob) chunkIndexAtOffset(o int64) (int, bool) {
	var i int
	if o >= ribsStoreChunkSize {
		i = int(o / ribsStoreChunkSize)
	}
	if i >= len(rsb.Chunks) {
		return -1, false
	}
	return i, true
}

func newRibsStoredBlobReader(sess ribs.Session, rsb *ribsStoredBlob) (*ribsStoredBlobReader, error) {
	return &ribsStoredBlobReader{
		sess: sess,
		blob: rsb,
	}, nil
}

func (r *ribsStoredBlobReader) Read(p []byte) (n int, err error) {
	if r.currentChunkReader == nil {
		if err := r.sess.View(context.TODO(),
			[]multihash.Multihash{r.blob.Chunks[r.currentChunkIndex].Hash()},
			func(_ int, data []byte) {
				//TODO: cache the retrieved bytes?
				//      but first, check if RIBS is already doing it.
				r.currentChunkReader = bytes.NewReader(data)
			}); err != nil {
			return 0, err
		}
	}
	if r.currentChunkPendingSeek > 0 {
		if _, err := r.currentChunkReader.Seek(r.currentChunkPendingSeek, io.SeekStart); err != nil {
			return 0, err
		}
		r.currentChunkPendingSeek = 0
	}
	read, err := r.currentChunkReader.Read(p)
	if err == io.EOF {
		if read < len(p) && r.currentChunkIndex+1 < len(r.blob.Chunks) {
			r.currentChunkIndex += 1
			r.currentChunkReader = nil
			readRemaining, err := r.Read(p[read:])
			return read + readRemaining, err
		}
	}
	return read, err
}

func (r *ribsStoredBlobReader) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = r.offset + offset
	case io.SeekEnd:
		newOffset = int64(r.blob.Size) + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}
	if newOffset < 0 {
		return 0, fmt.Errorf("offset too small: %d", offset)
	}
	if newOffset > int64(r.blob.Size) {
		return 0, fmt.Errorf("offset beyond size: %d", offset)
	}
	chunkIndex, found := r.blob.chunkIndexAtOffset(newOffset)
	if !found {
		return 0, fmt.Errorf("offset beyond size: %d", r.offset)
	}
	if chunkIndex != r.currentChunkIndex && r.currentChunkReader != nil {
		r.currentChunkReader = nil
	}
	r.offset = newOffset
	r.currentChunkIndex = chunkIndex
	r.currentChunkPendingSeek = newOffset % ribsStoreChunkSize
	return r.offset, nil
}

func (r *ribsStoredBlobReader) Close() error {
	return nil
}
