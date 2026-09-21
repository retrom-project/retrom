package importing

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"retrom/internal/cleanup"
)

var ErrNWJSExecutableInvalid = errors.New("NWJS_EXECUTABLE_INVALID")

const maximumPEHeaderOffset = 16 << 20

// ScanNWJSExecutable validates a Windows PE prefix before applying the shared
// ZIP scanner to the package.nw bytes appended by NW.js.
func ScanNWJSExecutable(
	ctx context.Context,
	path string,
	limits ArchiveLimits,
) ([]ArchiveEntry, error) {
	if err := ValidateNWJSExecutable(path); err != nil {
		return nil, err
	}
	return scanZIP(ctx, path, limits, nil)
}

// ScanNWJSExecutableWithConsumer preserves the one-pass CAS staging behavior
// used by project archives after validating the executable container.
func ScanNWJSExecutableWithConsumer(
	ctx context.Context,
	path string,
	limits ArchiveLimits,
	consumer ArchiveContentConsumer,
) ([]ArchiveEntry, error) {
	if consumer == nil {
		return nil, ErrArchiveUnsafe
	}
	if err := ValidateNWJSExecutable(path); err != nil {
		return nil, err
	}
	return scanZIP(ctx, path, limits, consumer)
}

// ValidateNWJSExecutable verifies that path is a Windows PE with an appended
// package.nw ZIP without reading or executing any native program bytes.
func ValidateNWJSExecutable(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open NW.js executable: %w", err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 68 {
		return ErrNWJSExecutableInvalid
	}
	minimumZIPOffset, err := validatePEPrefix(file, info.Size())
	if err != nil || !hasAppendedZIP(file, info.Size(), minimumZIPOffset) {
		return ErrNWJSExecutableInvalid
	}
	return nil
}

func validatePEPrefix(file *os.File, size int64) (int64, error) {
	var dosHeader [64]byte
	if _, err := file.ReadAt(dosHeader[:], 0); err != nil || string(dosHeader[:2]) != "MZ" {
		return 0, ErrNWJSExecutableInvalid
	}
	peOffset := int64(binary.LittleEndian.Uint32(dosHeader[0x3c:]))
	if peOffset < int64(len(dosHeader)) || peOffset > maximumPEHeaderOffset || peOffset > size-24 {
		return 0, ErrNWJSExecutableInvalid
	}
	var peHeader [24]byte
	if _, err := file.ReadAt(peHeader[:], peOffset); err != nil || string(peHeader[:4]) != "PE\x00\x00" {
		return 0, ErrNWJSExecutableInvalid
	}
	machine := binary.LittleEndian.Uint16(peHeader[4:6])
	sections := binary.LittleEndian.Uint16(peHeader[6:8])
	if !supportedPEMachine(machine) || sections == 0 || sections > 96 {
		return 0, ErrNWJSExecutableInvalid
	}
	return peOffset + int64(len(peHeader)), nil
}

func hasAppendedZIP(file *os.File, size, minimumOffset int64) bool {
	reader, err := zip.NewReader(file, size)
	if err != nil || len(reader.File) == 0 {
		return false
	}
	for _, entry := range reader.File {
		offset, offsetErr := entry.DataOffset()
		if offsetErr == nil && offset > minimumOffset && offset < size {
			return true
		}
	}
	return false
}

func supportedPEMachine(machine uint16) bool {
	return machine == 0x014c || machine == 0x8664 || machine == 0xaa64
}
