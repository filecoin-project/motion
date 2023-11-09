package blob

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-log/v2"
)

var (
	ErrBlobNotFound   = errors.New("no blob is found with given ID")
	ErrBlobTooLarge   = errors.New("blob size exceeds the maximum allowed")
	ErrNotEnoughSpace = errors.New("insufficient local storage space remaining")
)

var (
	logger = log.Logger("motion/blobstore")
)

type (
	// ID uniquely identifies a blob.
	ID uuid.UUID
	// Descriptor describes a created blob.
	Descriptor struct {
		// ID is the blob identifier.
		ID ID
		// Size is the size of blob in bytes.
		Size uint64
		// ModificationTime is the latest time at which the blob was modified.
		ModificationTime time.Time
		Replicas         []Replica
	}
	Replica struct {
		Provider string
		Pieces   []Piece
	}
	Piece struct {
		Expiration  time.Time
		LastUpdated time.Time
		PieceCID    string
		Status      string
	}
	Store interface {
		Put(context.Context, io.Reader) (*Descriptor, error)
		Describe(context.Context, ID) (*Descriptor, error)
		Get(context.Context, ID) (io.ReadSeekCloser, error)
	}
	PassThroughGet interface {
		PassGet(http.ResponseWriter, *http.Request, ID)
	}
)

// NewID instantiates a new randomly generated ID.
func NewID() (*ID, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	i := ID(id)
	return &i, nil
}

// String returns the string representation of ID.
func (i *ID) String() string {
	return uuid.UUID(*i).String()
}

// Decode instantiates the ID from the decoded string value.
func (i *ID) Decode(v string) error {
	id, err := uuid.Parse(v)
	if err != nil {
		return err
	}
	*i = ID(id)
	return nil
}
