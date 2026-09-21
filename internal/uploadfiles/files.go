package uploadfiles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type Store struct{ root string }

func New(root string) *Store { return &Store{root: filepath.Join(root, "tmp", "uploads")} }

type Temporary struct {
	*os.File
	directory string
}

func (store *Store) Create(upload, file string) (*Temporary, error) {
	directory := filepath.Join(store.root, upload, file)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create upload directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".part-")
	if err != nil {
		return nil, fmt.Errorf("create temporary upload part: %w", err)
	}
	return &Temporary{File: temporary, directory: directory}, nil
}

func (temporary *Temporary) Publish(name string) error {
	if err := os.Rename(temporary.Name(), filepath.Join(temporary.directory, name)); err != nil {
		return fmt.Errorf("publish upload part: %w", err)
	}
	return nil
}

func (store *Store) Open(key string) (io.ReadCloser, error) {
	file, err := os.Open(filepath.Join(store.root, filepath.FromSlash(key)))
	if err != nil {
		return nil, fmt.Errorf("open staged upload part: %w", err)
	}
	return file, nil
}

func (store *Store) Remove(ctx context.Context, upload, file string) error {
	target := filepath.Join(store.root, upload, file)
	paths := []string{}
	err := filepath.WalkDir(target, func(path string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("inspect upload cleanup: %w", err)
		}
		if errors.Is(walkErr, fs.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return fmt.Errorf("inspect upload path: %w", walkErr)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("enumerate upload cleanup: %w", err)
	}
	for index := len(paths) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stop upload cleanup: %w", err)
		}
		if err := os.Remove(paths[index]); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove staged upload: %w", err)
		}
	}
	return nil
}
