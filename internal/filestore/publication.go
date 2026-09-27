package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"retrom/internal/cleanup"
)

func (store *Store) DirectoryPublished(itemID, gameID string) (bool, error) {
	item, target := ItemDirectory(itemID), GameDirectory(gameID)
	if item == "" || target == "" {
		return false, ErrRecordInvalid
	}
	info, err := os.Lstat(filepath.Join(store.root, target))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect published directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrRecordInvalid
	}
	if _, err := os.Lstat(filepath.Join(store.root, item, "payload")); !errors.Is(err, os.ErrNotExist) {
		return false, ErrRecordInvalid
	}
	if err := store.checkParents(target); err != nil {
		return false, err
	}
	return true, nil
}

func (store *Store) existingCopy(ctx context.Context, record Record, target string) (Metadata, bool, error) {
	record.Path = target
	value, err := record.Encode()
	if err != nil {
		return Metadata{}, false, err
	}
	file, err := store.OpenRecord(value)
	if errors.Is(err, os.ErrNotExist) {
		return Metadata{}, false, nil
	}
	if err != nil {
		return Metadata{}, false, err
	}
	defer func() { cleanup.Error("close prepared copy", file.Close()) }()
	digest := sha256.New()
	size, err := io.Copy(digest, contextReader{ctx: ctx, source: file})
	if err != nil {
		return Metadata{}, false, fmt.Errorf("verify prepared copy: %w", err)
	}
	if size != record.Size || hex.EncodeToString(digest.Sum(nil)) != record.SHA256 {
		return Metadata{}, false, ErrRecordInvalid
	}
	return Metadata{
		Record: value, Path: filepath.Join(store.root, target), SHA256: record.SHA256, MD5: record.MD5,
		SHA1: record.SHA1, CRC32: record.CRC32, Size: record.Size,
	}, true, nil
}
