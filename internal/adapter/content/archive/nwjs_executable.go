package archive

import (
	"archive/zip"
	"context"
	"fmt"
	"os"

	"retrom/internal/capability/format/importing"
)

func (factory *Factory) ScanNWJSExecutable(
	ctx context.Context, path string, limits importing.ArchiveLimits,
) ([]importing.ArchiveEntry, error) {
	if err := factory.ValidateNWJSExecutable(ctx, path); err != nil {
		return nil, err
	}
	return factory.ScanZIP(ctx, path, limits)
}

// The context belongs to diagnostics; validation retains the old no-cancellation semantics.
func (factory *Factory) ValidateNWJSExecutable(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open NW.js executable: %w", err)
	}
	defer closeResource(ctx, factory.reporter, file)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 68 {
		return importing.ErrNWJSExecutableInvalid
	}
	minimum, err := validatePEPrefix(file, info.Size())
	if err != nil || !hasAppendedZIP(file, info.Size(), minimum) {
		return importing.ErrNWJSExecutableInvalid
	}
	return nil
}

func validatePEPrefix(file *os.File, size int64) (int64, error) {
	var dosHeader [64]byte
	if _, err := file.ReadAt(dosHeader[:], 0); err != nil {
		return 0, importing.ErrNWJSExecutableInvalid
	}
	offset, err := importing.PEHeaderOffset(dosHeader, size)
	if err != nil {
		return 0, importing.ErrNWJSExecutableInvalid
	}
	var peHeader [24]byte
	if _, err := file.ReadAt(peHeader[:], offset); err != nil {
		return 0, importing.ErrNWJSExecutableInvalid
	}
	minimum, err := importing.PEMinimumZIPOffset(peHeader, offset)
	if err != nil {
		return 0, importing.ErrNWJSExecutableInvalid
	}
	return minimum, nil
}

func hasAppendedZIP(file *os.File, size, minimumOffset int64) bool {
	reader, err := zip.NewReader(file, size)
	if err != nil || len(reader.File) == 0 {
		return false
	}
	for _, entry := range reader.File {
		offset, err := entry.DataOffset()
		if err == nil && offset > minimumOffset && offset < size {
			return true
		}
	}
	return false
}
