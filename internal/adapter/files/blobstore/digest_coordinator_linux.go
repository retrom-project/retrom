//go:build linux

package blobstore

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	blobmodel "retrom/internal/model/blob"
)

var (
	errInvalidBlobDigest = errors.New("digest lock requires a canonical lowercase SHA-256")
	errUnsafeDigestPath  = errors.New("digest lock path is not a controlled local directory or file")
	errDigestFilesystem  = errors.New("digest locks require a supported local filesystem")
)

// DigestCoordinatorOptions configures only the technical lock wait.
// Wait must honor cancellation; it does not sample or extend a business deadline.
type DigestCoordinatorOptions struct {
	Wait func(context.Context) error
}

// DigestCoordinator owns no persistent descriptors. Each Acquire opens its own
// lock files so separate callers in the same process also exclude one another.
type DigestCoordinator struct {
	root string
	wait func(context.Context) error
}

var _ blobmodel.DigestCoordinator = (*DigestCoordinator)(nil)

// OpenDigestCoordinator requires an existing data root owned by the process
// user. It validates the root and creates private technical lock directories.
func OpenDigestCoordinator(dataRoot string, options DigestCoordinatorOptions) (*DigestCoordinator, error) {
	if dataRoot == "" {
		return nil, errUnsafeDigestPath
	}
	root, err := filepath.Abs(dataRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve digest data root: %w", err)
	}
	directory, err := openDigestLockDirectory(root)
	if err != nil {
		return nil, err
	}
	if err := closeDigestDescriptor(directory); err != nil {
		return nil, err
	}
	wait := options.Wait
	if wait == nil {
		wait = waitForDigestLock
	}
	return &DigestCoordinator{root: root, wait: wait}, nil
}

// Acquire locks each selected stripe once, in ascending order. Callers must
// finish preparation before acquiring and must acquire before opening a writer.
func (coordinator *DigestCoordinator) Acquire(
	ctx context.Context,
	digests []string,
) (blobmodel.DigestLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire digest locks: %w", err)
	}
	stripes, err := digestLockStripes(digests)
	if err != nil {
		return nil, err
	}
	lease := &digestLease{descriptors: make([]int, 0, len(stripes))}
	if len(stripes) == 0 {
		return lease, nil
	}
	directory, err := openDigestLockDirectory(coordinator.root)
	if err != nil {
		return nil, err
	}
	for _, stripe := range stripes {
		if err := coordinator.acquireStripe(ctx, directory, stripe, lease); err != nil {
			return nil, errors.Join(err, lease.Release(), closeDigestDescriptor(directory))
		}
	}
	if err := closeDigestDescriptor(directory); err != nil {
		return nil, errors.Join(err, lease.Release())
	}
	return lease, nil
}

func (coordinator *DigestCoordinator) acquireStripe(
	ctx context.Context, directory int, stripe byte, lease *digestLease,
) error {
	descriptor, err := openDigestLockFile(directory, stripe)
	if err != nil {
		return err
	}
	// Include the descriptor before trying the lock so cancellation and any
	// syscall failure also close the last, possibly still unlocked file.
	lease.descriptors = append(lease.descriptors, descriptor)
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("acquire digest stripe: %w", err)
		}
		err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			return fmt.Errorf("lock digest stripe: %w", err)
		}
		if err := coordinator.wait(ctx); err != nil {
			return fmt.Errorf("wait for digest stripe: %w", err)
		}
	}
}

func digestLockStripes(digests []string) ([]byte, error) {
	var selected [256]bool
	for _, digest := range digests {
		if len(digest) != 64 || strings.ToLower(digest) != digest {
			return nil, errInvalidBlobDigest
		}
		decoded, err := hex.DecodeString(digest)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errInvalidBlobDigest, err)
		}
		selected[decoded[0]] = true
	}
	stripes := make([]byte, 0, len(selected))
	for stripe, present := range selected {
		if present {
			stripes = append(stripes, byte(stripe))
		}
	}
	return stripes, nil
}

func waitForDigestLock(ctx context.Context) error {
	const retryDelay = 5 * time.Millisecond
	timer := time.NewTimer(retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("cancel digest lock wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

type digestLease struct {
	descriptors []int
	once        sync.Once
	err         error
}

// Release closes in reverse acquisition order, which also releases flock even
// if a caller is unwinding after a failed acquisition. Lock files stay on disk.
func (lease *digestLease) Release() error {
	lease.once.Do(func() {
		for index := len(lease.descriptors) - 1; index >= 0; index-- {
			lease.err = errors.Join(lease.err, closeDigestDescriptor(lease.descriptors[index]))
		}
	})
	return lease.err
}

func closeDigestDescriptor(descriptor int) error {
	if err := unix.Close(descriptor); err != nil {
		return fmt.Errorf("close digest descriptor: %w", err)
	}
	return nil
}
