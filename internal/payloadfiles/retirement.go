package payloadfiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"retrom/internal/cleanup"
	application "retrom/internal/service/payloadrelease"
)

func (files *Store) paths(digest, jobID string) (string, string, error) {
	value, err := hex.DecodeString(digest)
	if files.blobs == nil || err != nil || len(value) != sha256.Size || jobID == "" {
		return "", "", application.ErrInputInvalid
	}
	canonical := files.blobs.Path(digest)
	identity := sha256.Sum256([]byte(jobID + "\x00" + digest))
	return canonical, filepath.Join(filepath.Dir(canonical), ".retired-"+hex.EncodeToString(identity[:])), nil
}

// Retire removes the canonical name while the database holds its write lock.
// The execution-specific hard link survives rollback and process interruption.
func (files *Store) Retire(ctx context.Context, digest, jobID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("retire Blob: %w", err)
	}
	canonical, retired, err := files.paths(digest, jobID)
	if err != nil {
		return err
	}
	linked := os.Link(canonical, retired)
	if errors.Is(linked, os.ErrNotExist) {
		return nil
	}
	if linked != nil && !errors.Is(linked, os.ErrExist) {
		return fmt.Errorf("retain garbage inode: %w", linked)
	}
	if errors.Is(linked, os.ErrExist) {
		same, err := sameRetiredFile(canonical, retired)
		if err != nil {
			return err
		}
		if !same {
			return nil
		}
	}

	if err := os.Remove(canonical); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("retire canonical Blob: %w", err)
	}
	return syncParent(retired)
}

func (files *Store) Restore(ctx context.Context, digest, jobID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("restore Blob: %w", err)
	}
	canonical, retired, err := files.paths(digest, jobID)
	if err != nil {
		return err
	}
	if err := os.Link(retired, canonical); err != nil && !errors.Is(err, os.ErrExist) && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("restore retired Blob: %w", err)
	}
	return files.DeleteRetired(ctx, digest, jobID)
}

func (files *Store) DeleteRetired(ctx context.Context, digest, jobID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("delete retired Blob: %w", err)
	}
	_, retired, err := files.paths(digest, jobID)
	if err != nil {
		return err
	}
	if err := os.Remove(retired); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("delete retired Blob: %w", err)
	}
	return syncParent(retired)
}

func syncParent(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open Blob directory for sync: %w", err)
	}
	defer func() { cleanup.Error("close Blob directory", directory.Close()) }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync retired Blob directory: %w", err)
	}
	return nil
}

// A retry can unlink only the inode retained by this execution.
func sameRetiredFile(canonical, retired string) (bool, error) {
	current, err := os.Lstat(canonical)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect canonical Blob: %w", err)
	}
	previous, err := os.Lstat(retired)
	if err != nil {
		return false, fmt.Errorf("inspect retired Blob: %w", err)
	}
	return os.SameFile(current, previous), nil
}
