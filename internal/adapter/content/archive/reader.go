package archive

import (
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/capability/format/importing"
)

type archiveReadMonitor struct {
	ctx     context.Context
	reader  io.Reader
	limit   int64
	written int64
	eof     bool
	prefix  [512]byte
}

func (monitor *archiveReadMonitor) Read(buffer []byte) (int, error) {
	if err := monitor.ctx.Err(); err != nil {
		return 0, fmt.Errorf("importing/archive: %w", err)
	}
	count, err := monitor.reader.Read(buffer)
	if count > 0 {
		if monitor.written+int64(count) > monitor.limit {
			return 0, importing.ErrArchiveLimitExceeded
		}
		if monitor.written < int64(len(monitor.prefix)) {
			copy(monitor.prefix[monitor.written:], buffer[:count])
		}
		monitor.written += int64(count)
	}
	if errors.Is(err, io.EOF) {
		monitor.eof = true
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("importing/archive: read member: %w", err)
	}
	return count, nil
}

func (monitor *archiveReadMonitor) observedPrefix() []byte {
	return monitor.prefix[:min(int64(len(monitor.prefix)), monitor.written)]
}

func copyExact(reader io.Reader, size int64) error {
	if size < 0 {
		return importing.ErrElectronASARInvalid
	}
	written, err := io.CopyN(io.Discard, reader, size)
	if err != nil || written != size {
		return importing.ErrElectronASARInvalid
	}
	return nil
}

func expectEOF(reader io.Reader) error {
	var single [1]byte
	count, err := reader.Read(single[:])
	if count != 0 || !errors.Is(err, io.EOF) {
		return importing.ErrElectronASARInvalid
	}
	return nil
}
