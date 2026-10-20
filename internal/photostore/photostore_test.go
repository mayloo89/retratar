package photostore_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/photostore"
)

func TestStore_RoundTrip(t *testing.T) {
	t.Parallel()
	s, err := photostore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key := photostore.NewKey()
	want := []byte("\xff\xd8not really a jpeg\xff\xd9")

	if err = s.Save(key, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	f, err := s.Open(key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got, err := io.ReadAll(f)
	f.Close() //nolint:errcheck // read-only file
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("read back %q, want %q", got, want)
	}

	if err = s.Remove(key); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err = s.Open(key); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open() after Remove error = %v, want not exist", err)
	}
	// Removing what is already gone is not an error.
	if err = s.Remove(key); err != nil {
		t.Errorf("second Remove() error = %v, want nil", err)
	}
}

func TestStore_SaveLeavesOnlyTheFinalFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := photostore.New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key := photostore.NewKey()
	if err = s.Save(key, []byte("x")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != key+".jpg" {
		t.Fatalf("directory = %v, want only %s.jpg", entries, key)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestStore_RejectsBadKeys(t *testing.T) {
	t.Parallel()
	good := photostore.NewKey()
	keys := map[string]string{
		"empty":       "",
		"traversal":   "../etc/passwd",
		"upper case":  strings.ToUpper(good),
		"25 chars":    good[:25],
		"27 chars":    good + "a",
		"slash":       "a/b",
		"extension":   "a.jpg",
		"digit 1":     strings.Repeat("1", 26),
		"nul":         good[:25] + "\x00",
		"traversal26": strings.Repeat("../", 8) + "xx",
	}

	for name, key := range keys {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := filepath.Join(root, "photos")
			s, err := photostore.New(dir)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if photostore.ValidKey(key) {
				t.Fatalf("ValidKey(%q) = true, want false", key)
			}
			if err = s.Save(key, []byte("x")); !errors.Is(err, photostore.ErrInvalidKey) {
				t.Errorf("Save() error = %v, want ErrInvalidKey", err)
			}
			if f, openErr := s.Open(key); !errors.Is(openErr, photostore.ErrInvalidKey) {
				if f != nil {
					f.Close() //nolint:errcheck // test cleanup
				}
				t.Errorf("Open() error = %v, want ErrInvalidKey", openErr)
			}
			if err = s.Remove(key); !errors.Is(err, photostore.ErrInvalidKey) {
				t.Errorf("Remove() error = %v, want ErrInvalidKey", err)
			}

			// Nothing may appear anywhere outside the photo directory, and
			// nothing at all inside it.
			var found []string
			err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if p != root && p != dir {
					found = append(found, p)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(found) != 0 {
				t.Errorf("files created: %v, want none", found)
			}
		})
	}
}

func TestNew_UnwritableDir(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := photostore.New(dir); err == nil {
		t.Fatal("New() error = nil, want an error for a read-only directory")
	}
}

func TestNew_CreatesTheDirectory(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "a", "b")
	if _, err := photostore.New(dir); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("directory has %d entries after New, want 0 (the probe must be removed)", len(entries))
	}
}

func TestNewKey(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for range 1000 {
		k := photostore.NewKey()
		if !photostore.ValidKey(k) {
			t.Fatalf("NewKey() = %q, not valid", k)
		}
		if seen[k] {
			t.Fatalf("NewKey() repeated %q", k)
		}
		seen[k] = true
	}
}
