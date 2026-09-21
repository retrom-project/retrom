package materializer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"retrom/internal/importing"
)

const MaxRGSSUncompressedBytes = int64(2147483647)

var ErrInvalid = errors.New("RPG_MATERIALIZATION_INVALID")

type SourceFile struct {
	Path string
	Size int64
	Open func() (io.ReadCloser, error)
}

type Result struct {
	SHA256           string
	SizeBytes        int64
	UncompressedSize int64
}

// WriteMKXPZ produces the deterministic stored ZIP consumed by the fixed
// mkxp-z adapter. Callers should write to a temporary destination and commit it
// only after this function succeeds.
func WriteMKXPZ(destination io.Writer, files []SourceFile) (Result, error) {
	if destination == nil || len(files) == 0 {
		return Result{}, ErrInvalid
	}
	ordered, totalBytes, err := validateSources(files)
	if err != nil {
		return Result{}, err
	}
	hasher := sha256.New()
	counting := &countingWriter{writer: io.MultiWriter(destination, hasher)}
	archive := zip.NewWriter(counting)
	for _, file := range ordered {
		if err := writeSource(archive, file); err != nil {
			_ = archive.Close()
			return Result{}, err
		}
	}
	if err := archive.Close(); err != nil {
		return Result{}, fmt.Errorf("%w: close mkxpz: %w", ErrInvalid, err)
	}
	return Result{
		SHA256: hex.EncodeToString(hasher.Sum(nil)), SizeBytes: counting.written,
		UncompressedSize: totalBytes,
	}, nil
}

func StoreZIPHeader(name string) *zip.FileHeader {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o644)
	header.Modified = time.Time{}
	// archive/zip otherwise emits an extended timestamp. These DOS fields encode
	// 1980-01-01 00:00:00 while keeping Extra empty.
	header.ModifiedDate = 33 //nolint:staticcheck // Deterministic ZIP wire contract.
	header.ModifiedTime = 0  //nolint:staticcheck // Deterministic ZIP wire contract.
	header.Extra = nil
	header.Comment = ""
	return header
}

func validateSources(files []SourceFile) ([]SourceFile, int64, error) {
	ordered := append([]SourceFile(nil), files...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Path < ordered[right].Path })
	keys := make(map[string]string, len(ordered))
	var totalBytes int64
	for _, file := range ordered {
		if _, err := importing.ValidateLogicalPath(file.Path); err != nil || file.Size < 0 || file.Open == nil ||
			file.Size > MaxRGSSUncompressedBytes-totalBytes {
			return nil, 0, ErrInvalid
		}
		key := cases.Fold().String(norm.NFKC.String(file.Path))
		if previous, exists := keys[key]; exists {
			return nil, 0, fmt.Errorf("%w: %q collides with %q", ErrInvalid, previous, file.Path)
		}
		keys[key] = file.Path
		totalBytes += file.Size
	}
	return ordered, totalBytes, nil
}

func writeSource(archive *zip.Writer, file SourceFile) error {
	destination, err := archive.CreateHeader(StoreZIPHeader(file.Path))
	if err != nil {
		return fmt.Errorf("%w: create %s: %w", ErrInvalid, file.Path, err)
	}
	source, err := file.Open()
	if err != nil {
		return fmt.Errorf("%w: open %s: %w", ErrInvalid, file.Path, err)
	}
	if source == nil {
		return fmt.Errorf("%w: open %s returned no reader", ErrInvalid, file.Path)
	}
	limited := &io.LimitedReader{R: source, N: file.Size + 1}
	written, copyErr := io.Copy(destination, limited)
	closeErr := source.Close()
	if copyErr != nil || closeErr != nil || written != file.Size {
		return fmt.Errorf("%w: read %s", ErrInvalid, file.Path)
	}
	return nil
}

type countingWriter struct {
	writer  io.Writer
	written int64
}

func (writer *countingWriter) Write(contents []byte) (int, error) {
	written, err := writer.writer.Write(contents)
	writer.written += int64(written)
	if err != nil {
		return written, fmt.Errorf("write mkxpz: %w", err)
	}
	return written, nil
}
