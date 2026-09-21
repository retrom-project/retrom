package processlock

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"retrom/internal/cleanup"
)

var (
	ErrAlreadyRunning    = errors.New("DATA_ROOT_LOCKED")
	errDescriptorInvalid = errors.New("PROCESS_LOCK_DESCRIPTOR_INVALID")
)

type Lock struct {
	file *os.File
}

func Acquire(dataDir string) (*Lock, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data root for lock: %w", err)
	}
	path := filepath.Join(dataDir, "retrom.lock")
	file, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_RDWR,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open process lock: %w", err)
	}
	descriptor, err := checkedFileDescriptor(file)
	if err != nil {
		cleanup.Error("close", file.Close())
		return nil, err
	}
	if err := syscall.Flock(descriptor, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		cleanup.Error("close", file.Close())
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("acquire process lock: %w", err)
	}
	if err := file.Truncate(0); err == nil {
		_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
		_ = file.Sync()
	}
	return &Lock{file: file}, nil
}

func (lock *Lock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	descriptor, descriptorErr := checkedFileDescriptor(lock.file)
	if descriptorErr != nil {
		cleanup.Error("close", lock.file.Close())
		return descriptorErr
	}
	unlockErr := syscall.Flock(descriptor, syscall.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return fmt.Errorf("release process lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close process lock: %w", closeErr)
	}
	return nil
}

func checkedFileDescriptor(file *os.File) (int, error) {
	descriptor := file.Fd()
	if uint64(descriptor) > uint64(math.MaxInt) {
		return 0, errDescriptorInvalid
	}
	return int(descriptor), nil
}
