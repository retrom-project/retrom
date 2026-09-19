package processlock

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"retrom/internal/model/diagnostics"
	model "retrom/internal/model/maintenance"
)

var errDescriptorInvalid = errors.New("PROCESS_LOCK_DESCRIPTOR_INVALID")

type Lock struct {
	ctx      context.Context
	file     *os.File
	reporter diagnostics.ErrorReporter
}

// Locker acquires the application's nonblocking, process-wide data-root lease.
type Locker struct {
	reporter diagnostics.ErrorReporter
}

// New binds the diagnostic sink before any resource is acquired.
func New(reporter diagnostics.ErrorReporter) *Locker {
	if reporter == nil {
		panic("process locker requires a diagnostic reporter")
	}
	return &Locker{reporter: reporter}
}

var (
	_ model.DataRootLocker = (*Locker)(nil)
	_ model.DataRootLease  = (*Lock)(nil)
)

func (locker *Locker) Acquire(ctx context.Context, dataDir string) (model.DataRootLease, error) {
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
		reportClose(ctx, locker.reporter, file.Close())
		return nil, err
	}
	if err := syscall.Flock(descriptor, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		reportClose(ctx, locker.reporter, file.Close())
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, model.ErrDataRootLocked
		}
		return nil, fmt.Errorf("acquire process lock: %w", err)
	}
	if err := file.Truncate(0); err == nil {
		_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
		_ = file.Sync()
	}
	return &Lock{ctx: ctx, file: file, reporter: locker.reporter}, nil
}

func (lock *Lock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	descriptor, descriptorErr := checkedFileDescriptor(lock.file)
	if descriptorErr != nil {
		reportClose(lock.ctx, lock.reporter, lock.file.Close())
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

// reportClose preserves the primary failure and never formats an error message.
func reportClose(ctx context.Context, reporter diagnostics.ErrorReporter, err error) {
	if err != nil {
		reporter.Report(ctx, diagnostics.CleanupFailure("close", "", fmt.Sprintf("%T", err)))
	}
}
