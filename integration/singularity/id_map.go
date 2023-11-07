package singularity

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/filecoin-project/motion/blob"
)

type idMap struct {
	dir string
}

func newIDMap(dir string) *idMap {
	return &idMap{
		dir: dir,
	}
}

func (im *idMap) path(blobID blob.ID) string {
	return path.Join(im.dir, blobID.String()+".id")
}

// Inserts a blob ID to Singularity ID mapping.
func (im *idMap) insert(blobID blob.ID, singularityID int64) error {
	idFile, err := os.CreateTemp(im.dir, "motion_local_store_*.id.temp")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		if err := idFile.Close(); err != nil {
			logger.Debugw("Failed to close temporary file", "err", err)
		}
	}()
	_, err = idFile.Write([]byte(strconv.FormatUint(uint64(singularityID), 10)))
	if err != nil {
		if err := os.Remove(idFile.Name()); err != nil {
			logger.Debugw("Failed to remove temporary file", "path", idFile.Name(), "err", err)
		}
		return fmt.Errorf("failed to write ID file: %w", err)
	}
	if err = os.Rename(idFile.Name(), im.path(blobID)); err != nil {
		return fmt.Errorf("failed to move ID file to store: %w", err)
	}

	return nil
}

// Maps blob ID to Singularity ID. Returns blob.ErrBlobNotFound if no mapping
// exists.
func (im *idMap) get(blobID blob.ID) (int64, error) {
	fileIDString, err := os.ReadFile(filepath.Join(im.dir, blobID.String()+".id"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, blob.ErrBlobNotFound
		}

		return 0, fmt.Errorf("could not read ID file: %w", err)
	}

	fileID, err := strconv.ParseUint(string(fileIDString), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("could not parse Singularity file ID '%s' of ID file for blob '%s': %w", fileIDString, blobID, err)
	}

	return int64(fileID), nil
}

// TODO: currently commented to silence unused warning
// // Removes blob ID to Singularity ID mapping. If no ID file existed,
// // blob.ErrBlobNotFound will be returned.
// func (im *idMap) remove(blobID blob.ID) error {
// 	if err := os.Remove(im.path(blobID)); err != nil {
// 		if errors.Is(err, os.ErrNotExist) {
// 			return blob.ErrBlobNotFound
// 		}

// 		return fmt.Errorf("could not read ID file: %w", err)
// 	}

// 	return nil
// }
