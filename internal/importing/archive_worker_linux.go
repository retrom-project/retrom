//go:build linux

package importing

import (
	"fmt"
	"runtime/debug"

	"golang.org/x/sys/unix"
)

const (
	archiveWorkerHeapLimitBytes    = 512 << 20
	archiveWorkerAddressSpaceBytes = 4 << 30
)

func applyArchiveWorkerLimits() error {
	debug.SetMemoryLimit(archiveWorkerHeapLimitBytes)
	limits := []struct {
		resource int
		current  uint64
		maximum  uint64
	}{
		// A full Retrom binary plus Go's reserved arenas and a 7z LZMA2 dictionary
		// can exceed 2 GiB of virtual address space while the live heap remains small.
		// The independent heap limit is the actual memory-consumption boundary.
		{unix.RLIMIT_AS, archiveWorkerAddressSpaceBytes, archiveWorkerAddressSpaceBytes},
		{unix.RLIMIT_CPU, 120, 120},
		{unix.RLIMIT_FSIZE, 8 << 30, 8 << 30},
		{unix.RLIMIT_NOFILE, 64, 64},
		{unix.RLIMIT_CORE, 0, 0},
	}
	for _, item := range limits {
		if err := unix.Setrlimit(item.resource, &unix.Rlimit{Cur: item.current, Max: item.maximum}); err != nil {
			return fmt.Errorf("set archive worker rlimit %d: %w", item.resource, err)
		}
	}
	return nil
}
