package filestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

func ItemDirectory(id string) string { return objectDirectory("staging/items", id) }
func GameDirectory(id string) string {
	if !validObjectID(id) {
		return ""
	}
	return "files/" + id[len(id)-2:] + "/" + id
}

func objectDirectory(kind, id string) string {
	if !validObjectID(id) {
		return ""
	}
	return kind + "/" + id
}

func validObjectID(id string) bool {
	value, err := uuid.Parse(id)
	return err == nil && value.String() == id
}

// CopyTo prepares one independent file in its final relative workspace layout.
// It runs before the domain write transaction.
func (store *Store) CopyTo(ctx context.Context, value, directory, name string) (Metadata, error) {
	record, err := ParseRecord(value)
	if err != nil {
		return Metadata{}, err
	}
	if !safeRelativePath(directory) || !safeRelativePath(name) {
		return Metadata{}, ErrRecordInvalid
	}
	target := path.Join(directory, name)
	if metadata, found, err := store.existingCopy(ctx, record, target); err != nil || found {
		return metadata, err
	}
	source, err := store.OpenRecord(value)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { cleanup.Error("close directory input", source.Close()) }()
	if err := store.makeDirectory(path.Dir(target)); err != nil {
		return Metadata{}, err
	}
	return store.copyDirectoryFile(ctx, record, target, source)
}

func (store *Store) copyDirectoryFile(ctx context.Context, record Record, target string,
	source *os.File,
) (Metadata, error) {
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return Metadata{}, fmt.Errorf("open data root: %w", err)
	}
	defer func() { cleanup.Error("close data root", root.Close()) }()
	candidate, err := store.Stage(contextReader{ctx: ctx, source: source})
	if err != nil {
		return Metadata{}, err
	}
	defer func() { cleanup.Error("discard workspace staging file", candidate.Discard()) }()
	metadata := candidate.Metadata()
	if metadata.SHA256 != record.SHA256 || metadata.Size != record.Size {
		return Metadata{}, ErrRecordInvalid
	}
	temporary, err := filepath.Rel(store.root, candidate.temporary)
	if err != nil {
		return Metadata{}, fmt.Errorf("copy to: %w", err)
	}
	if err := root.Link(temporary, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			metadata, _, verifyErr := store.existingCopy(ctx, record, target)
			return metadata, verifyErr
		}
		return Metadata{}, fmt.Errorf("commit workspace file: %w", err)
	}

	if err := syncDirectory(filepath.Join(store.root, path.Dir(target))); err != nil {
		return Metadata{}, err
	}
	record.Path = target
	encoded, err := record.Encode()
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{
		Record: encoded, Path: filepath.Join(store.root, target), SHA256: record.SHA256,
		MD5: record.MD5, SHA1: record.SHA1, CRC32: record.CRC32, Size: record.Size,
	}, nil
}

func (store *Store) makeDirectory(relative string) error {
	if !safeRelativePath(relative) {
		return ErrRecordInvalid
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return fmt.Errorf("open data directory: %w", err)
	}
	defer func() { cleanup.Error("close data directory", root.Close()) }()
	if err := root.MkdirAll(relative, 0o700); err != nil {
		return fmt.Errorf("create workspace directory: %w", err)
	}
	return store.checkParents(relative + "/file")
}

// PublishDirectory is idempotent for an already recorded publication intent.
// Both identifiers must come from that intent, never from a caller's new retry.
func (store *Store) PublishDirectory(itemID, gameID string) error {
	item, target := ItemDirectory(itemID), GameDirectory(gameID)
	if item == "" || target == "" {
		return ErrRecordInvalid
	}
	source := item + "/payload"
	if err := store.makeDirectory(path.Dir(target)); err != nil {
		return err
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return fmt.Errorf("open publication root: %w", err)
	}
	defer func() { cleanup.Error("close publication root", root.Close()) }()
	from, sourceErr := root.Lstat(source)
	to, targetErr := root.Lstat(target)
	if errors.Is(sourceErr, os.ErrNotExist) && targetErr == nil && to.IsDir() {
		return syncDirectory(filepath.Join(store.root, path.Dir(target)))
	}
	if sourceErr != nil {
		return fmt.Errorf("read publication workspace: %w", sourceErr)
	}
	if !from.IsDir() || from.Mode()&os.ModeSymlink != 0 || !errors.Is(targetErr, os.ErrNotExist) {
		return ErrRecordInvalid
	}
	if err := store.checkParents(source); err != nil {
		return err
	}
	if err := root.Rename(source, target); err != nil {
		return fmt.Errorf("publish game directory: %w", err)
	}
	if err := syncDirectory(filepath.Join(store.root, item)); err != nil {
		return err
	}
	return syncDirectory(filepath.Join(store.root, path.Dir(target)))
}

func PublishedRecord(value, itemID, gameID string) (string, error) {
	record, err := ParseRecord(value)
	if err != nil {
		return "", err
	}
	source, target := ItemDirectory(itemID), GameDirectory(gameID)
	if source == "" || target == "" {
		return "", ErrRecordInvalid
	}
	relative, err := filepath.Rel(source+"/payload", record.Path)
	if err != nil || !safeRelativePath(relative) {
		return "", ErrRecordInvalid
	}
	record.Path = path.Join(target, relative)
	return record.Encode()
}
