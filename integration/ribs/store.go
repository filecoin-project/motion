package ribs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/motion/blob"
	"github.com/google/uuid"
	chunk "github.com/ipfs/boxo/chunker"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/lotus-web3/ribs"
	"github.com/lotus-web3/ribs/rbdeal"
	"github.com/lotus-web3/ribs/ributil"
	"github.com/multiformats/go-multihash"
)

// TODO parameterize this.
const storeChunkSize = 1 << 20 // 1 MiB

var (
	_ blob.Store        = (*Store)(nil)
	_ io.ReadSeekCloser = (*storedBlobReader)(nil)
)

type (
	// Store is an experimental Store implementation that uses RIBS.
	// See: https://github.com/filcat/ribs
	Store struct {
		ribs     ribs.RIBS
		maxSize  int
		indexDir string
	}
	storedBlob struct {
		*blob.Descriptor
		Chunks []cid.Cid `json:"chunks"`
	}
	storedBlobReader struct {
		sess   ribs.Session
		blob   *storedBlob
		offset int64

		currentChunkIndex       int
		currentChunkReader      *bytes.Reader
		currentChunkPendingSeek int64
	}
)

// NewStore instantiates a new experimental RIBS store.
func NewStore(dir string, ks types.KeyStore) (*Store, error) {
	dir = filepath.Clean(dir)
	rbdealDir := filepath.Join(dir, "rbdeal")
	if err := os.Mkdir(rbdealDir, 0750); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("failed to create RIBS deal directory: %w", err)
	}
	indexDir := filepath.Join(dir, "index")
	if err := os.Mkdir(indexDir, 0750); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("failed to create RIBS internal directory: %w", err)
	}

	rbs, err := rbdeal.Open(rbdealDir,
		rbdeal.WithLocalWalletOpener(func(string) (*ributil.LocalWallet, error) {
			return ributil.NewWallet(ks)
		}))

	if err != nil {
		return nil, err
	}
	return &Store{
		ribs:     rbs,
		maxSize:  31 << 30, // 31 GiB
		indexDir: indexDir,
	}, nil

}

func (s *Store) Start(_ context.Context) error {
	// TODO: change RIBS to take context.
	return s.ribs.Start()
}
func (s *Store) Put(ctx context.Context, in io.Reader) (*blob.Descriptor, error) {

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
	batch := s.ribs.Session(ctx).Batch(ctx)

	splitter := chunk.NewSizeSplitter(in, storeChunkSize)

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
			if size > s.maxSize {
				return nil, blob.ErrBlobTooLarge
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
	storedBlob := &storedBlob{
		Descriptor: &blob.Descriptor{
			ID:               blob.ID(id),
			Size:             uint64(size),
			ModificationTime: modtime,
		},
		Chunks: chunkCids,
	}
	index, err := os.Create(filepath.Join(s.indexDir, id.String()))
	if err != nil {
		return nil, err
	}
	defer index.Close()
	if err = json.NewEncoder(index).Encode(storedBlob); err != nil {
		return nil, err
	}
	return storedBlob.Descriptor, nil
}

func (s *Store) Get(ctx context.Context, id blob.ID) (io.ReadSeekCloser, error) {
	storedBlob, err := s.describeStoredBlob(ctx, id)
	if err != nil {
		return nil, err
	}
	session := s.ribs.Session(ctx)
	reader, err := newStoredBlobReader(session, storedBlob)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func (s *Store) Describe(ctx context.Context, id blob.ID) (*blob.Descriptor, error) {
	storedBlob, err := s.describeStoredBlob(ctx, id)
	if err != nil {
		return nil, err
	}
	return storedBlob.Descriptor, err
}

func (s *Store) describeStoredBlob(_ context.Context, id blob.ID) (*storedBlob, error) {
	index, err := os.Open(filepath.Join(s.indexDir, id.String()))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, blob.ErrBlobNotFound
		}
		return nil, err
	}
	defer index.Close()

	var storedBlob storedBlob
	err = json.NewDecoder(index).Decode(&storedBlob)
	// TODO: populate descriptor status with Filecoin chain data about the stored blob.
	return &storedBlob, err
}

func (s *Store) Shutdown(_ context.Context) error {
	// TODO: change RIBS to take context.
	return s.ribs.Close()
}

func (rsb *storedBlob) chunkIndexAtOffset(o int64) (int, bool) {
	var i int
	if o >= storeChunkSize {
		i = int(o / storeChunkSize)
	}
	if i >= len(rsb.Chunks) {
		return -1, false
	}
	return i, true
}

func newStoredBlobReader(sess ribs.Session, rsb *storedBlob) (*storedBlobReader, error) {
	return &storedBlobReader{
		sess: sess,
		blob: rsb,
	}, nil
}

func (r *storedBlobReader) Read(p []byte) (n int, err error) {
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

func (r *storedBlobReader) Seek(offset int64, whence int) (int64, error) {
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
	r.currentChunkPendingSeek = newOffset % storeChunkSize
	return r.offset, nil
}

func (r *storedBlobReader) Close() error {
	return nil
}
