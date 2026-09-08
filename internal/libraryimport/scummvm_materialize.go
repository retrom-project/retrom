package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentmanifest"
	"retrom/internal/importing"
	"retrom/internal/rpgmaker/fileset"
	"retrom/internal/scummvm"
)

func (service *Service) detectScummVMTree(
	ctx context.Context,
	files []fileset.SourceFile,
	metadata map[int]blobstore.Metadata,
	archive *importSourceFile,
) (scummvm.Snapshot, error) {
	root, err := os.MkdirTemp("", "retrom-scummvm-input-")
	if err != nil {
		return scummvm.Snapshot{}, fmt.Errorf("materialize ScummVM directory: %w", err)
	}
	defer func() { cleanup.Error("remove ScummVM input", os.RemoveAll(root)) }()
	manifestFiles, err := materializeScummVMInputs(ctx, root, files, metadata, archive)
	if err != nil {
		return scummvm.Snapshot{}, err
	}
	_, digest, err := contentmanifest.Build(scummvm.ContentKind, manifestFiles)
	if err != nil {
		return scummvm.Snapshot{}, fmt.Errorf("digest ScummVM input: %w", err)
	}
	result, err := service.scummVMDetector.Detect(ctx, root, digest)
	if err != nil {
		return scummvm.Snapshot{}, fmt.Errorf("detect ScummVM input: %w", err)
	}
	snapshot, err := scummvm.NewSnapshot(result)
	if err != nil {
		return scummvm.Snapshot{}, fmt.Errorf("validate ScummVM detection: %w", err)
	}
	return snapshot, nil
}

func materializeScummVMInputs(
	ctx context.Context,
	root string,
	files []fileset.SourceFile,
	metadata map[int]blobstore.Metadata,
	archive *importSourceFile,
) ([]contentmanifest.File, error) {
	manifest := make([]contentmanifest.File, 0, len(files))
	limits := importing.DefaultArchiveLimits()
	var total int64
	for _, file := range files {
		value, exists := metadata[file.SourceIndex]
		if !exists || value.Size != file.SizeBytes || value.Size < 0 ||
			value.Size > limits.MaxEntryBytes || value.Size > limits.MaxExpandedBytes-total {
			return nil, scummvm.ErrInputInvalid
		}
		total += value.Size
		if err := copyScummVMInput(ctx, value, filepath.Join(root, filepath.FromSlash(file.Path))); err != nil {
			return nil, err
		}
		entry := contentmanifest.File{
			Role: "PROJECT_FILE", LogicalName: file.Path, BlobSHA256: value.SHA256, SizeBytes: value.Size,
		}
		if archive != nil {
			ordinal := file.SourceIndex
			entry.SourceArchiveSHA256 = &archive.sha256
			entry.SourceArchiveEntryOrdinal = &ordinal
		}
		manifest = append(manifest, entry)
	}
	return manifest, nil
}

func copyScummVMInput(ctx context.Context, metadata blobstore.Metadata, destination string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("copy ScummVM input: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create ScummVM input directory: %w", err)
	}
	input, err := os.Open(metadata.Path)
	if err != nil {
		return fmt.Errorf("read ScummVM source: %w", err)
	}
	defer func() { cleanup.Error("close ScummVM input", input.Close()) }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
	if err != nil {
		return fmt.Errorf("create ScummVM input: %w", err)
	}
	defer func() { cleanup.Error("close ScummVM output", output.Close()) }()
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(output, hasher),
		io.LimitReader(scummVMContextReader{ctx: ctx, reader: input}, metadata.Size+1))
	if err != nil {
		return fmt.Errorf("copy ScummVM input: %w", err)
	}
	if size != metadata.Size || hex.EncodeToString(hasher.Sum(nil)) != metadata.SHA256 {
		return scummvm.ErrInputInvalid
	}
	return nil
}

type scummVMContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader scummVMContextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, fmt.Errorf("read canceled ScummVM input: %w", err)
	}
	size, err := reader.reader.Read(data)
	if err == io.EOF {
		return size, io.EOF
	}
	if err != nil {
		return size, fmt.Errorf("read ScummVM source: %w", err)
	}
	return size, nil
}
