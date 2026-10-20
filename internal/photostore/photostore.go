// Package photostore keeps processed photos on local disk, one file per photo,
// named by a random key.
//
// The key is the only thing that ever becomes part of a path. It is generated
// here, stored in the database, and arrives from URLs as well, so every method
// checks it with [ValidKey] first: that check, and nothing else, stands between
// a request and the filesystem.
package photostore

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrInvalidKey is returned for a key that does not match [ValidKey].
var ErrInvalidKey = errors.New("photostore: invalid key")

const (
	keyBytes = 16 // 128 bits of randomness
	keyLen   = 26 // base32 of 16 bytes, unpadded: ceil(16*8/5)

	fileMode = 0o640
	dirMode  = 0o750
	ext      = ".jpg"
)

// keyPattern is the same pattern as the CHECK on users.photo_key.
var keyPattern = regexp.MustCompile(`^[a-z2-7]{26}$`)

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Store is a directory of photos.
type Store struct{ dir string }

// New returns a Store over dir, creating it if needed and checking that it is
// writable, so a bad volume fails at startup and not on the first upload.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("photostore: create %s: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return nil, fmt.Errorf("photostore: %s is not writable: %w", dir, err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return nil, fmt.Errorf("photostore: probe %s: %w", dir, err)
	}
	if err := os.Remove(name); err != nil {
		return nil, fmt.Errorf("photostore: probe %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// NewKey returns a fresh random key: 26 characters from [a-z2-7].
func NewKey() string {
	var b [keyBytes]byte
	rand.Read(b[:]) //nolint:errcheck // crypto/rand.Read never returns an error
	return strings.ToLower(encoding.EncodeToString(b[:]))
}

// ValidKey reports whether key has the shape [NewKey] produces.
func ValidKey(key string) bool {
	return len(key) == keyLen && keyPattern.MatchString(key)
}

func (s *Store) path(key string) (string, error) {
	if !ValidKey(key) {
		return "", ErrInvalidKey
	}
	return filepath.Join(s.dir, key+ext), nil
}

// Save writes jpeg under key. It writes a temporary file in the same
// directory and renames it into place, so a reader sees all of the file or
// none of it.
func (s *Store) Save(key string, jpeg []byte) error {
	dst, err := s.path(key)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("photostore: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(jpeg); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("photostore: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("photostore: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("photostore: close: %w", err)
	}
	// CreateTemp makes the file 0600; the group read bit is for whoever backs
	// the volume up.
	if err := os.Chmod(tmpName, fileMode); err != nil {
		cleanup()
		return fmt.Errorf("photostore: chmod: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		cleanup()
		return fmt.Errorf("photostore: rename: %w", err)
	}
	// A rename is recorded in the directory, not in the file. Without this sync
	// a power cut after the database commit can lose the rename, leaving a row
	// that names a missing file. Failing here fails the upload before the
	// commit, which is the safe direction.
	d, openErr := os.Open(s.dir)
	if openErr != nil {
		return fmt.Errorf("photostore: open dir: %w", openErr)
	}
	if syncErr := d.Sync(); syncErr != nil {
		_ = d.Close()
		return fmt.Errorf("photostore: sync dir: %w", syncErr)
	}
	if closeErr := d.Close(); closeErr != nil {
		return fmt.Errorf("photostore: close dir: %w", closeErr)
	}
	return nil
}

// Open opens the photo stored under key. A missing photo is an error that
// satisfies errors.Is(err, fs.ErrNotExist).
func (s *Store) Open(key string) (*os.File, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p) //nolint:gosec // G304: p is built from a key that passed ValidKey
}

// Remove deletes the photo stored under key. A photo that is already gone is
// not an error.
func (s *Store) Remove(key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("photostore: remove: %w", err)
	}
	return nil
}
