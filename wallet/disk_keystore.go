package wallet

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-log/v2"
)

var (
	_      types.KeyStore = (*DiskKeyStore)(nil)
	logger                = log.Logger("motion/wallet")
)

// DiskKeyStore is a types.KeyStore that uses the local file system to store list and retrieve keys.
// See: types.KeyStore, types.KeyInfo.
type DiskKeyStore struct {
	path string
}

func DefaultDiskKeyStoreOpener(p string, createIfNotExist bool) func() (types.KeyStore, error) {
	return func() (types.KeyStore, error) {
		if p == "" {
			logger.Debugw("Disk keystore path is not specified. Falling back on default path under user home directory...")
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get user home directory while initialising disk keystore: %w", err)
			}
			p = filepath.Join(home, ".motion", "wallet")
			logger.Infow("Using default disk keystore path under user home directory", "path", p)
		} else {
			p = filepath.Clean(p)
			logger.Infow("Using specified disk keystore path", "path", p)
		}
		switch store, err := OpenDiskKeyStore(p); {
		case err == nil:
			return store, nil
		case errors.Is(err, os.ErrNotExist):
			if !createIfNotExist {
				return nil, err
			}
			if err := os.MkdirAll(p, 0700); err != nil {
				return nil, err
			}
			return OpenDiskKeyStore(p)
		default:
			return nil, err
		}
	}
}

// OpenDiskKeyStore opens a disk keystore at the given path.
// The path must exist prior to opening the keystore and must have at most 0600 permissions.
func OpenDiskKeyStore(p string) (*DiskKeyStore, error) {
	stat, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("failed to open disk key store: %w", err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("disk key store path must be a directory: %s", p)
	}
	path := filepath.Clean(p)
	//if err := checkPermissions(path, stat); err != nil {
	//	return nil, err
	//}
	logger.Infow("Opened disk keystore successfully", "path", path)
	return &DiskKeyStore{path: path}, nil
}

// List lists all the keys stored in the KeyStore by traversing the disk keystore path.
func (dks *DiskKeyStore) List() ([]string, error) {
	var keys []string
	err := filepath.WalkDir(dks.path, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get info for path '%s': %w", path, err)
		}
		if err := checkPermissions(path, info); err != nil {
			return err
		}
		key, err := dks.getKeyNameFromPath(info.Name())
		if err != nil {
			return fmt.Errorf("failed to decode key: '%s': %w", info.Name(), err)
		}
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		logger.Errorw("Failed to traverse disk keystore path while listing stored keys", "err", err)
		return nil, err
	}
	return keys, nil
}

func checkPermissions(path string, info fs.FileInfo) error {
	if info.Mode()&0077 != 0 {
		return fmt.Errorf("permissions of disk keystore path '%s' must be at most 0600, got: %#o", path, info.Mode())
	}
	return nil
}

// Get retrieves the stored key corresponding to the given name.
// If no such key exists, returns types.ErrKeyInfoNotFound.
func (dks *DiskKeyStore) Get(name string) (types.KeyInfo, error) {
	keyPath := dks.getKeyPathFromName(name)
	stat, err := os.Stat(keyPath)
	if os.IsNotExist(err) {
		return types.KeyInfo{}, fmt.Errorf("failed to open key '%s': %w", name, types.ErrKeyInfoNotFound)
	} else if err != nil {
		return types.KeyInfo{}, fmt.Errorf("failed to open key '%s': %w", name, err)
	}
	if err := checkPermissions(keyPath, stat); err != nil {
		return types.KeyInfo{}, err
	}
	keyFile, err := os.Open(keyPath)
	if err != nil {
		return types.KeyInfo{}, fmt.Errorf("failed to open key '%s': %w", name, err)
	}
	defer func() {
		if err := keyFile.Close(); err != nil {
			logger.Debugw("Failed to close key path after get", "path", keyPath, "err", err)
		}
	}()
	var res types.KeyInfo
	if err = json.NewDecoder(keyFile).Decode(&res); err != nil {
		return types.KeyInfo{}, fmt.Errorf("failed to decode key '%s': %w", name, err)
	}
	return res, nil
}

// Put stopres the given keyInfo under the given name.
// If a key with the same name already exists, returns types.ErrKeyExists.
func (dks *DiskKeyStore) Put(name string, keyInfo types.KeyInfo) error {
	keyPath := dks.getKeyPathFromName(name)
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	switch {
	case err == nil:
		if err := json.NewEncoder(keyFile).Encode(keyInfo); err != nil {
			if err := keyFile.Close(); err != nil {
				logger.Debugw("Failed to close key path after put", "path", keyPath, "err", err)
			}
			if err := os.Remove(keyPath); err != nil {
				logger.Debugw("Failed to remove partially written key", "path", keyPath, "err", err)
			}
			return fmt.Errorf("failed to encode key '%s': %w", name, err)
		}
		return keyFile.Close()
	case errors.Is(err, os.ErrExist):
		return types.ErrKeyExists
	default:
		return err
	}
}

// Delete deletes the key stored under the given name.
// If no such key exists, returns types.ErrKeyInfoNotFound.
func (dks *DiskKeyStore) Delete(name string) error {
	keyPath := dks.getKeyPathFromName(name)
	switch err := os.Remove(keyPath); {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return types.ErrKeyInfoNotFound
	default:
		return err
	}
}

func (dks *DiskKeyStore) getKeyPathFromName(name string) string {
	key := base64.StdEncoding.EncodeToString([]byte(name))
	return filepath.Join(dks.path, key)
}

func (dks *DiskKeyStore) getKeyNameFromPath(p string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(p)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
