package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/data-preservation-programs/singularity/client"
	"github.com/data-preservation-programs/singularity/handler/dataset"
	"github.com/data-preservation-programs/singularity/handler/datasource"
)

const motionDatasetName = "MOTION_DATASET"
const maxCarSize = "31.5GiB"

type SingularityStore struct {
	local             *LocalStore
	sourceID          uint32
	singularityClient client.Client
}

func NewSingularityStore(dir string, singularityClient client.Client) *SingularityStore {
	local := NewLocalStore(dir)
	return &SingularityStore{
		local:             local,
		singularityClient: singularityClient,
	}
}

func (l *SingularityStore) Start(ctx context.Context) error {
	_, err := l.singularityClient.CreateDataset(ctx, dataset.CreateRequest{
		Name:       motionDatasetName,
		MaxSizeStr: maxCarSize,
	})
	var asDuplicatedRecord client.DuplicateRecordError

	// return errors, but ignore duplicated record, that means we just already created it
	if err != nil && !errors.As(err, &asDuplicatedRecord) {
		return err
	}
	source, err := l.singularityClient.CreateLocalSource(ctx, motionDatasetName, datasource.LocalRequest{
		SourcePath:        l.local.dir,
		RescanInterval:    "0",
		DeleteAfterExport: false,
	})
	// handle source already created
	if errors.As(err, &asDuplicatedRecord) {
		sources, err := l.singularityClient.ListSourcesByDataset(ctx, motionDatasetName)
		if err != nil {
			return err
		}
		for _, source := range sources {
			if source.Path == strings.TrimSuffix(l.local.dir, "/") {
				l.sourceID = source.ID
				return nil
			}
		}
		// this shouldn't happen - if we have a duplicate, the record should exist
		return errors.New("unable to locate dataset")
	}
	// return errors, but ignore duplicated record, that means we just already created it
	if err != nil {
		return err
	}
	l.sourceID = source.ID
	return nil
}

func (l *SingularityStore) Shutdown(_ context.Context) error {
	return nil
}

func (s *SingularityStore) Put(ctx context.Context, reader io.ReadCloser) (*Descriptor, error) {
	desc, err := s.local.Put(ctx, reader)
	if err != nil {
		return nil, err
	}
	model, err := s.singularityClient.PushItem(ctx, s.sourceID, datasource.ItemInfo{Path: desc.ID.String() + ".bin"})
	if err != nil {
		return nil, fmt.Errorf("error creating singularity entry: %w", err)
	}
	idFile, err := os.CreateTemp(s.local.dir, "motion_local_store_*.bin.temp")
	if err != nil {
		return nil, err
	}
	defer idFile.Close()
	_, err = idFile.Write([]byte(strconv.FormatUint(model.ID, 10)))
	if err != nil {
		_ = os.Remove(idFile.Name())
		return nil, err
	}
	if err = os.Rename(idFile.Name(), path.Join(s.local.dir, desc.ID.String()+".id")); err != nil {
		return nil, err
	}
	return desc, nil
}

func (s *SingularityStore) Get(ctx context.Context, id ID) (io.ReadSeekCloser, error) {
	// this is largely artificial -- we're verifying the singularity item, but just reading from
	// the local store
	idStream, err := os.Open(path.Join(s.local.dir, id.String()+".id"))
	if err != nil {
		return nil, err
	}
	itemIDString, err := io.ReadAll(idStream)
	if err != nil {
		return nil, err
	}
	itemID, err := strconv.ParseUint(string(itemIDString), 10, 64)
	if err != nil {
		return nil, err
	}
	item, err := s.singularityClient.GetItem(ctx, itemID)
	var asNotFoundError client.NotFoundError
	if errors.As(err, &asNotFoundError) {
		return nil, ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("error loading singularity entry: %w", err)
	}
	var decoded ID
	err = decoded.Decode(strings.TrimSuffix(path.Base(item.Path), path.Ext(item.Path)))
	if err != nil {
		return nil, err
	}
	return s.local.Get(ctx, decoded)
}

func (s *SingularityStore) Describe(ctx context.Context, id ID) (*Descriptor, error) {
	// this is largely artificial -- we're verifying the singularity item, but just reading from
	// the local store
	idStream, err := os.Open(path.Join(s.local.dir, id.String()+".id"))
	if err != nil {
		return nil, err
	}
	itemIDString, err := io.ReadAll(idStream)
	if err != nil {
		return nil, err
	}
	itemID, err := strconv.ParseUint(string(itemIDString), 10, 64)
	if err != nil {
		return nil, err
	}
	item, err := s.singularityClient.GetItem(ctx, itemID)
	var asNotFoundError client.NotFoundError
	if errors.As(err, &asNotFoundError) {
		return nil, ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("error loading singularity entry: %w", err)
	}
	var decoded ID
	err = decoded.Decode(strings.TrimSuffix(path.Base(item.Path), path.Ext(item.Path)))
	if err != nil {
		return nil, err
	}
	return s.local.Describe(ctx, decoded)
}
