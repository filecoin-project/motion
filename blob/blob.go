package blob

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

var (
	ErrBlobTooLarge = errors.New("blob size exceeds the maximum allowed")
	ErrBlobNotFound = errors.New("no blob is found with given ID")
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
	}
	Store interface {
		Put(context.Context, io.ReadCloser) (*Descriptor, error)
		Get(context.Context, ID) (io.ReadSeekCloser, *Descriptor, error)
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
