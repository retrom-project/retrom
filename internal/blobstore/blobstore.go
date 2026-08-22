package blobstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"

	"retrom/internal/cleanup"
	"retrom/internal/legacychecksum"
)

var errExistingObjectIntegrity = errors.New("existing CAS object failed integrity check")

type Metadata struct {
	SHA256   string
	MD5      string
	SHA1     string
	CRC32    string
	Size     int64
	Path     string
	Existing bool
}

type Store struct {
	root string
	tmp  string
}

func Open(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "blobs", "sha256")
	temporary := filepath.Join(dataDir, "tmp", "jobs")
	for _, directory := range []string{root, temporary} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create blob directory: %w", err)
		}
	}
	return &Store{root: root, tmp: temporary}, nil
}

func (store *Store) Put(source io.Reader) (Metadata, error) {
	temporary, err := os.CreateTemp(store.tmp, ".blob-")
	if err != nil {
		return Metadata{}, fmt.Errorf("create blob candidate: %w", err)
	}
	name := temporary.Name()
	defer cleanup.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		cleanup.Error("close", temporary.Close())
		return Metadata{}, fmt.Errorf("secure blob candidate: %w", err)
	}
	sha256Hash := sha256.New()
	legacyHashes := legacychecksum.New()
	crc32Hash := crc32.NewIEEE()
	written, err := io.Copy(io.MultiWriter(temporary, sha256Hash, legacyHashes.MD5, legacyHashes.SHA1, crc32Hash), source)
	if err != nil {
		cleanup.Error("close", temporary.Close())
		return Metadata{}, fmt.Errorf("write blob candidate: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup.Error("close", temporary.Close())
		return Metadata{}, fmt.Errorf("sync blob candidate: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Metadata{}, fmt.Errorf("close blob candidate: %w", err)
	}
	sha256Value := hex.EncodeToString(sha256Hash.Sum(nil))
	target := store.Path(sha256Value)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return Metadata{}, fmt.Errorf("create blob shard: %w", err)
	}
	existing := false
	if err := os.Link(name, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return Metadata{}, fmt.Errorf("publish blob: %w", err)
		}
		existing = true
		if err := verifyExisting(target, written, sha256Value); err != nil {
			return Metadata{}, err
		}
	}
	if err := syncDirectory(filepath.Dir(target)); err != nil {
		return Metadata{}, err
	}
	return Metadata{
		SHA256: sha256Value, MD5: hex.EncodeToString(legacyHashes.MD5.Sum(nil)),
		SHA1: hex.EncodeToString(legacyHashes.SHA1.Sum(nil)), CRC32: hex.EncodeToString(crc32Hash.Sum(nil)),
		Size: written, Path: target, Existing: existing,
	}, nil
}

func (store *Store) Path(digest string) string {
	if len(digest) != 64 {
		return ""
	}
	return filepath.Join(store.root, digest[:2], digest[2:4], digest)
}

func (store *Store) OpenDigest(digest string) (*os.File, error) {
	path := store.Path(digest)
	if path == "" {
		return nil, os.ErrNotExist
	}
	// The path is derived solely from a validated 64-byte lowercase digest and the configured CAS root.
	file, err := os.OpenFile( //nolint:gosec // Validated digest path; O_NOFOLLOW blocks symlinks.
		path,
		os.O_RDONLY|syscallNoFollow(),
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open blob: %w", err)
	}
	return file, nil
}

func verifyExisting(path string, size int64, expectedDigest string) error {
	// The caller produced this path with Store.Path from a locally computed SHA-256 digest.
	file, err := os.OpenFile( //nolint:gosec // Locally derived digest path with O_NOFOLLOW.
		path,
		os.O_RDONLY|syscallNoFollow(),
		0,
	)
	if err != nil {
		return fmt.Errorf("open existing blob: %w", err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	digest := sha256.New()
	actualSize, err := io.Copy(digest, file)
	if err != nil || actualSize != size || hex.EncodeToString(digest.Sum(nil)) != expectedDigest {
		return errExistingObjectIntegrity
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path) //nolint:gosec // Path is the parent shard of a digest-derived CAS object.
	if err != nil {
		return fmt.Errorf("open blob directory: %w", err)
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync blob directory: %w", err)
	}
	return nil
}
