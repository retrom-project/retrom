package providersync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// lock holds an exclusive advisory lock until release; it serializes writers of one directory.
func lock(directory string) (func(), error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", directory, err)
	}
	path := filepath.Join(directory, ".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if errors.Is(err, os.ErrPermission) {
		// A volume prepared by another user (for example on the host) keeps its lock file;
		// flock works on a read-only descriptor, so the container can still serialize on it.
		file, err = os.Open(path)
	}
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock %s: %w", directory, err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

// writeAtomic publishes bytes durably: write, fsync, rename, fsync the directory.
func writeAtomic(path string, contents []byte) error {
	pending := path + ".pending"
	file, err := os.OpenFile(pending, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", pending, err)
	}
	_, writeErr := file.Write(contents)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(pending)
		return fmt.Errorf("write %s: %w", pending, err)
	}
	if err = os.Rename(pending, path); err != nil {
		return fmt.Errorf("publish %s: %w", path, err)
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open %s: %w", directory, err)
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	if err = errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync %s: %w", directory, err)
	}
	return nil
}

// linkOrCopy shares the verified cache inode; a cache on another filesystem falls back to a copy.
func linkOrCopy(source, target string) error {
	err := os.Link(source, target)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("link %s: %w", target, err)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if err = errors.Join(copyErr, closeErr); err != nil {
		return fmt.Errorf("copy %s: %w", target, err)
	}
	return nil
}

// freeSpace is replaceable in tests to simulate a full filesystem.
var freeSpace = freeBytes

func freeBytes(directory string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(directory, &stat); err != nil {
		return 0, fmt.Errorf("stat filesystem %s: %w", directory, err)
	}
	return int64(stat.Bavail) * stat.Bsize, nil
}

// fetchVerified streams one asset into writer, accepting only its exact size and digest.
func fetchVerified(ctx context.Context, source Source, asset Asset, writer io.Writer) error {
	body, err := source.Fetch(ctx, asset.Asset)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", asset.Asset, err)
	}
	defer func() { _ = body.Close() }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(writer, hash), io.LimitReader(body, asset.SizeBytes+1))
	if err != nil {
		return fmt.Errorf("read %s: %w: %w", asset.Asset, ErrFetchFailed, err)
	}
	if written != asset.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return fmt.Errorf("verify %s: %w", asset.Asset, ErrChunkInvalid)
	}
	return nil
}

func fetchBytes(ctx context.Context, source Source, asset Asset) ([]byte, error) {
	var buffer bytes.Buffer
	if err := fetchVerified(ctx, source, asset, &buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
