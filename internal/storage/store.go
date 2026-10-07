package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"retrom/internal/model"

	"github.com/google/uuid"
)

type (
	Store struct {
		root      *os.Root
		directory string
	}
	File struct {
		Key        string
		SHA256     string
		Size       int64
		PreparedAt time.Time
	}
)

const MaximumFileSize int64 = 8 * 1024 * 1024 * 1024

var errTooLarge = errors.New("file exceeds size limit")

func Open(directory string) (*Store, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create managed directory: %w", err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open managed directory: %w", err)
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve managed root: %w", err)
	}
	return &Store{root: root, directory: absolute}, nil
}

func (s *Store) Close() error {
	if err := s.root.Close(); err != nil {
		return fmt.Errorf("close managed directory: %w", err)
	}
	return nil
}

func SafeRelative(value string) bool {
	return value != "" && len(value) <= 1024 && !strings.ContainsAny(value, "\\\x00") &&
		!strings.HasPrefix(value, "/") && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../")
}

func OwnerKey(kind, id string) (string, error) {
	if !model.OneOf(kind, "games", "saves", "bios") {
		return "", model.ErrInvalid
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return "", model.ErrInvalid
	}
	return kind + "/" + id, nil
}

func (s *Store) Write(ctx context.Context, kind, id string, reader io.Reader, maximum int64) (File, error) {
	owner, err := OwnerKey(kind, id)
	if err != nil {
		return File{}, fmt.Errorf("managed owner: %w", err)
	}
	if maximum < 1 || maximum > MaximumFileSize {
		return File{}, model.ErrInvalid
	}
	if err = s.root.MkdirAll("temporary", 0o700); err != nil {
		return File{}, fmt.Errorf("create temporary directory: %w", err)
	}
	token := uuid.NewString()
	temporary := "temporary/" + token
	key := owner + "/" + token
	prepared, err := s.copyTemporary(ctx, temporary, reader, maximum)
	if err != nil {
		return File{}, err
	}
	if err = ctx.Err(); err != nil {
		return File{}, fmt.Errorf("prepare managed file: %w", err)
	}
	if err = s.publish(temporary, key, owner); err != nil {
		return File{}, err
	}
	prepared.Key = key
	prepared.PreparedAt = time.Now()
	return prepared, nil
}

func (s *Store) copyTemporary(ctx context.Context, key string, reader io.Reader, maximum int64) (File, error) {
	file, err := s.root.OpenFile(key, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return File{}, fmt.Errorf("create temporary file: %w", err)
	}
	defer closeFile(file)
	hash := sha256.New()
	source := io.LimitReader(&contextReader{ctx: ctx, reader: reader}, maximum+1)
	copied, err := io.Copy(io.MultiWriter(file, hash), source)
	if err != nil {
		return File{}, fmt.Errorf("write managed file: %w", err)
	}
	if copied > maximum {
		return File{}, errTooLarge
	}
	if err = file.Sync(); err != nil {
		return File{}, fmt.Errorf("sync managed file: %w", err)
	}
	return File{SHA256: hex.EncodeToString(hash.Sum(nil)), Size: copied}, nil
}

func (s *Store) publish(temporary, key, owner string) error {
	if err := s.root.MkdirAll(owner, 0o700); err != nil {
		return fmt.Errorf("create owner directory: %w", err)
	}
	if err := s.syncDirectory(path.Dir(owner)); err != nil {
		return err
	}
	if err := s.syncDirectory("."); err != nil {
		return err
	}
	if err := s.root.Rename(temporary, key); err != nil {
		return fmt.Errorf("publish managed file: %w", err)
	}
	if err := s.syncDirectory(owner); err != nil {
		return err
	}
	return s.syncDirectory("temporary")
}

func (s *Store) syncDirectory(key string) error {
	directory, err := s.root.Open(key)
	if err != nil {
		return fmt.Errorf("open managed directory: %w", err)
	}
	defer closeFile(directory)
	if err = directory.Sync(); err != nil {
		return fmt.Errorf("sync managed directory: %w", err)
	}
	return nil
}

func (s *Store) Read(key string) (*os.File, error) {
	if !SafeRelative(key) {
		return nil, model.ErrInvalid
	}
	file, err := s.root.Open(key)
	if err != nil {
		return nil, fmt.Errorf("read managed file: %w", err)
	}
	return file, nil
}

func (s *Store) Remove(key string) error {
	if !SafeRelative(key) {
		return model.ErrInvalid
	}
	if err := s.root.Remove(key); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove managed file: %w", err)
	}
	parts := strings.Split(key, "/")
	if len(parts) == 3 {
		owner, err := OwnerKey(parts[0], parts[1])
		if err == nil {
			if err = s.root.Remove(owner); err != nil && !errors.Is(err, os.ErrNotExist) &&
				!errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
				return fmt.Errorf("remove empty managed owner: %w", err)
			}
		}
	}
	return nil
}

func (s *Store) RemoveOwner(kind, id string) error {
	key, err := OwnerKey(kind, id)
	if err != nil {
		return fmt.Errorf("remove owner: %w", err)
	}
	if err = s.root.RemoveAll(key); err != nil {
		return fmt.Errorf("remove owner files: %w", err)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, fmt.Errorf("read cancelled: %w", err)
	}
	count, err := r.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("read source: %w", err)
	}
	return count, nil
}

func (s *Store) Absolute(key string) (string, error) {
	if !SafeRelative(key) {
		return "", model.ErrInvalid
	}
	return filepath.Join(s.directory, filepath.FromSlash(key)), nil
}

func (s *Store) TemporaryPath() (string, string, error) {
	if err := s.root.MkdirAll("temporary", 0o700); err != nil {
		return "", "", fmt.Errorf("create resource assembly directory: %w", err)
	}
	key := "temporary/" + uuid.NewString()
	absolute, err := s.Absolute(key)
	return key, absolute, err
}
