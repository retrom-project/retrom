//go:build linux

package blobstore

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const digestDirectoryFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

func openDigestLockDirectory(root string) (int, error) {
	descriptor, err := openDigestDataRoot(root)
	if err != nil {
		return -1, err
	}
	for _, name := range []string{".locks", "blobs"} {
		child, err := openPrivateDigestDirectory(descriptor, name)
		closeErr := closeDigestDescriptor(descriptor)
		if err != nil {
			return -1, errors.Join(err, closeErr)
		}
		if closeErr != nil {
			return -1, errors.Join(closeErr, closeDigestDescriptor(child))
		}
		descriptor = child
	}
	return descriptor, nil
}

func openDigestDataRoot(root string) (int, error) {
	descriptor, err := unix.Open("/", digestDirectoryFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("open digest filesystem root: %w", err)
	}
	for _, name := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		if name == "" {
			continue
		}
		if err := validateDigestAncestor(descriptor); err != nil {
			return -1, errors.Join(err, closeDigestDescriptor(descriptor))
		}
		child, err := unix.Openat(descriptor, name, digestDirectoryFlags, 0)
		closeErr := closeDigestDescriptor(descriptor)
		if err != nil {
			return -1, errors.Join(fmt.Errorf("open digest data root component: %w", err), closeErr)
		}
		if closeErr != nil {
			return -1, errors.Join(closeErr, closeDigestDescriptor(child))
		}
		descriptor = child
	}
	if err := validateDigestDataRoot(descriptor); err != nil {
		return -1, errors.Join(err, closeDigestDescriptor(descriptor))
	}
	return descriptor, nil
}

func validateDigestAncestor(descriptor int) error {
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		return fmt.Errorf("inspect digest ancestor: %w", err)
	}
	owned := status.Uid == 0 || status.Uid == uint32(os.Geteuid())
	writable := status.Mode&0o022 != 0
	sticky := status.Mode&unix.S_ISVTX != 0
	if !owned || (writable && !sticky) {
		return errUnsafeDigestPath
	}
	return nil
}

func validateDigestDataRoot(descriptor int) error {
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		return fmt.Errorf("inspect digest data root: %w", err)
	}
	if status.Uid != uint32(os.Geteuid()) || status.Mode&0o022 != 0 {
		return errUnsafeDigestPath
	}
	return validateDigestFilesystem(descriptor)
}

func validateDigestFilesystem(descriptor int) error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(descriptor, &filesystem); err != nil {
		return fmt.Errorf("inspect digest filesystem: %w", err)
	}
	switch filesystem.Type {
	case unix.EXT4_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.XFS_SUPER_MAGIC,
		unix.TMPFS_MAGIC, unix.RAMFS_MAGIC, unix.OVERLAYFS_SUPER_MAGIC, unix.F2FS_SUPER_MAGIC:
		return nil
	default:
		return fmt.Errorf("%w: type 0x%x", errDigestFilesystem, filesystem.Type)
	}
}

func openPrivateDigestDirectory(parent int, name string) (int, error) {
	if err := unix.Mkdirat(parent, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, fmt.Errorf("create digest lock directory: %w", err)
	}
	descriptor, err := unix.Openat(parent, name, digestDirectoryFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("open digest lock directory: %w", err)
	}
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		return -1, errors.Join(fmt.Errorf("inspect digest lock directory: %w", err), closeDigestDescriptor(descriptor))
	}
	if status.Uid != uint32(os.Geteuid()) || status.Mode&0o7777 != 0o700 {
		return -1, errors.Join(errUnsafeDigestPath, closeDigestDescriptor(descriptor))
	}
	if err := validateDigestFilesystem(descriptor); err != nil {
		return -1, errors.Join(err, closeDigestDescriptor(descriptor))
	}
	return descriptor, nil
}

func openDigestLockFile(directory int, stripe byte) (int, error) {
	name := fmt.Sprintf("%02x.lock", stripe)
	const flags = unix.O_RDWR | unix.O_CREAT | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOCTTY
	descriptor, err := unix.Openat(directory, name, flags, 0o600)
	if err != nil {
		return -1, fmt.Errorf("open digest lock file: %w", err)
	}
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		return -1, errors.Join(fmt.Errorf("inspect digest lock file: %w", err), closeDigestDescriptor(descriptor))
	}
	if status.Mode&unix.S_IFMT != unix.S_IFREG || status.Mode&0o7777 != 0o600 ||
		status.Uid != uint32(os.Geteuid()) || status.Nlink != 1 {
		return -1, errors.Join(errUnsafeDigestPath, closeDigestDescriptor(descriptor))
	}
	if err := validateDigestFilesystem(descriptor); err != nil {
		return -1, errors.Join(err, closeDigestDescriptor(descriptor))
	}
	return descriptor, nil
}
