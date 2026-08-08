//go:build linux

package importing

import (
	"fmt"
	"runtime/debug"

	"golang.org/x/sys/unix"
)

func applyArchiveWorkerLimits() error {
	debug.SetMemoryLimit(512 << 20)
	limits := []struct {
		resource int
		current  uint64
		maximum  uint64
	}{
		{unix.RLIMIT_AS, 2 << 30, 2 << 30},
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
