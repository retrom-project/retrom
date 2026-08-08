//go:build !linux

package importing

func applyArchiveWorkerLimits() error {
	return ErrArchiveSandboxUnavailable
}
