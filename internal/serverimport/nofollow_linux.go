//go:build linux

package serverimport

import (
	"fmt"
	"io/fs"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

func openDirectoryNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open root directory without following links: %w", err)
	}
	return fileFromDescriptor(fd, path), nil
}

func openDirectoryAt(parent *os.File, name string) (*os.File, error) {
	parentFD, err := checkedDescriptor(parent)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(
		parentFD,
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open child directory without following links: %w", err)
	}
	return fileFromDescriptor(fd, name), nil
}

func openRegularFileAt(parent *os.File, name string) (*os.File, error) {
	parentFD, err := checkedDescriptor(parent)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(
		parentFD,
		name,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open regular file without following links: %w", err)
	}
	return fileFromDescriptor(fd, name), nil
}

func inspectEntryAt(parent *os.File, name string) (fs.FileInfo, error) {
	parentFD, err := checkedDescriptor(parent)
	if err != nil {
		return nil, err
	}
	descriptor, err := unix.Openat(parentFD, name, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("inspect child without following links: %w", err)
	}
	handle := fileFromDescriptor(descriptor, name)
	defer func() { _ = handle.Close() }()
	info, err := handle.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat child descriptor: %w", err)
	}
	return info, nil
}

func duplicateDirectory(parent *os.File) (*os.File, error) {
	fd, err := unix.FcntlInt(parent.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate directory descriptor: %w", err)
	}
	return fileFromDescriptor(fd, parent.Name()), nil
}

func checkedDescriptor(file *os.File) (int, error) {
	descriptor := file.Fd()
	if descriptor > uintptr(math.MaxInt) {
		return 0, fmt.Errorf("directory descriptor exceeds int range: %w", ErrRootUnavailable)
	}
	return int(descriptor), nil
}

func fileFromDescriptor(descriptor int, name string) *os.File {
	// unix.Open/Openat/FcntlInt return either a non-negative descriptor or an
	// error; the conversion therefore cannot wrap.
	return os.NewFile(uintptr(descriptor), name) //nolint:gosec // Validated Unix file descriptor.
}
