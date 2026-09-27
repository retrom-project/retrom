package filestore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"retrom/internal/cleanup"
	"retrom/internal/legacychecksum"

	"github.com/google/uuid"
)

var errCandidateClosed = errors.New("file candidate is closed")

type Metadata struct {
	Record string
	SHA256 string
	MD5    string
	SHA1   string
	CRC32  string
	Size   int64
	Path   string
}

type Store struct {
	publicationMu sync.Mutex
	root          string
	tmp           string
}

// Candidate holds verified bytes in the job staging directory until the
// caller knows that a domain object will reference them.
type Candidate struct {
	store     *Store
	temporary string
	metadata  Metadata
	closed    bool
}

func Open(dataDir string) (*Store, error) {
	root := dataDir
	temporary := filepath.Join(dataDir, "staging", "writes")
	for _, directory := range []string{root, temporary} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create file directory: %w", err)
		}
	}
	return &Store{root: root, tmp: temporary}, nil
}

func (store *Store) Put(source io.Reader) (Metadata, error) {
	candidate, err := store.Stage(source)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { cleanup.Error("discard file candidate", candidate.Discard()) }()
	return candidate.Commit()
}

func (store *Store) Stage(source io.Reader) (*Candidate, error) {
	temporary, err := os.CreateTemp(store.tmp, ".file-")
	if err != nil {
		return nil, fmt.Errorf("create file candidate: %w", err)
	}
	name := temporary.Name()
	success := false
	defer func() {
		if !success {
			cleanup.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("secure file candidate: %w", err)
	}
	sha256Hash := sha256.New()
	legacyHashes := legacychecksum.New()
	crc32Hash := crc32.NewIEEE()
	written, err := io.Copy(
		io.MultiWriter(temporary, sha256Hash, legacyHashes.MD5, legacyHashes.SHA1, crc32Hash),
		source,
	)
	if err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("write file candidate: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("sync file candidate: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("close file candidate: %w", err)
	}
	sha256Value := hex.EncodeToString(sha256Hash.Sum(nil))
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("allocate file identity: %w", err)
	}
	metadata := Metadata{
		Record: id.String(),
		SHA256: sha256Value, MD5: hex.EncodeToString(legacyHashes.MD5.Sum(nil)),
		SHA1: hex.EncodeToString(legacyHashes.SHA1.Sum(nil)), CRC32: hex.EncodeToString(crc32Hash.Sum(nil)),
		Size: written, Path: name,
	}
	sample := make([]byte, 512)
	probe, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("inspect staged media: %w", err)
	}
	count, readErr := probe.Read(sample)
	cleanup.Error("close media probe", probe.Close())
	if readErr != nil && readErr != io.EOF {
		return nil, fmt.Errorf("read media probe: %w", readErr)
	}
	record := Record{
		Path: "staging/writes/" + id.String(), SHA256: metadata.SHA256,
		MD5: metadata.MD5, SHA1: metadata.SHA1, CRC32: metadata.CRC32, Size: written,
		MediaType: http.DetectContentType(sample[:count]),
	}
	metadata.Record, err = record.Encode()
	if err != nil {
		return nil, err
	}
	success = true
	return &Candidate{store: store, temporary: name, metadata: metadata}, nil
}

func (candidate *Candidate) Metadata() Metadata {
	return candidate.metadata
}

func (candidate *Candidate) Commit() (Metadata, error) {
	if candidate == nil || candidate.closed {
		return Metadata{}, errCandidateClosed
	}
	target := candidate.store.Path(candidate.metadata.Record)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return Metadata{}, fmt.Errorf("create file shard: %w", err)
	}
	if err := os.Link(candidate.temporary, target); err != nil {
		return Metadata{}, fmt.Errorf("publish independent file: %w", err)
	}
	if err := syncDirectory(filepath.Dir(target)); err != nil {
		return Metadata{}, err
	}
	if err := os.Remove(candidate.temporary); err != nil {
		return Metadata{}, fmt.Errorf("remove committed file candidate: %w", err)
	}
	candidate.closed = true
	candidate.metadata.Path = target
	return candidate.metadata, nil
}

func (candidate *Candidate) Discard() error {
	if candidate == nil || candidate.closed {
		return nil
	}
	err := os.Remove(candidate.temporary)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("discard file candidate: %w", err)
	}
	candidate.closed = true
	return nil
}

// Path resolves the relative path carried by a domain's file record.
func (store *Store) Path(value string) string {
	record, err := ParseRecord(value)
	if err != nil {
		return ""
	}
	return filepath.Join(store.root, filepath.FromSlash(record.Path))
}

func (store *Store) OpenRecord(id string) (*os.File, error) {
	path := store.Path(id)
	if path == "" {
		return nil, os.ErrNotExist
	}
	record, err := ParseRecord(id)
	if err != nil {
		return nil, err
	}
	if err := store.checkParents(record.Path); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return nil, fmt.Errorf("open content root: %w", err)
	}
	defer func() { cleanup.Error("close content root", root.Close()) }()
	file, err := root.OpenFile(record.Path, os.O_RDONLY|syscallNoFollow(), 0)
	if err != nil {
		return nil, fmt.Errorf("open stored file: %w", err)
	}
	return file, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file directory: %w", err)
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync file directory: %w", err)
	}
	return nil
}

// Every component is checked because O_NOFOLLOW only protects the final component.
func (store *Store) checkParents(relative string) error {
	if !safeRelativePath(relative) {
		return ErrRecordInvalid
	}
	current := store.root
	parts := strings.Split(relative, "/")
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect file parent: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrRecordInvalid
		}
	}
	return nil
}

// LockPublication serializes the short publication phase for this data root.
func (store *Store) LockPublication() func() {
	store.publicationMu.Lock()
	return store.publicationMu.Unlock
}
