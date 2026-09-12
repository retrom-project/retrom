package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
)

func (service *Service) stageUploadPart(
	uploadID, fileID string,
	number int,
	span byteRange,
	expected string,
	body io.Reader,
) (int64, error) {
	directory := filepath.Join(service.dataDir, "tmp", "uploads", uploadID, fileID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return 0, fmt.Errorf("create upload directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".part-")
	if err != nil {
		return 0, fmt.Errorf("create temporary upload part: %w", err)
	}
	defer cleanup.Remove(temporary.Name())
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(body, span.end-span.start+2))
	closeErr := temporary.Close()
	if copyErr != nil {
		return 0, fmt.Errorf("%w: receive part: %w", ErrInvalid, copyErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close upload part: %w", closeErr)
	}
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if written != span.end-span.start+1 || actualDigest != expected {
		return 0, ErrInvalid
	}

	target := filepath.Join(directory, strconv.Itoa(number)+"-"+expected)
	if err := os.Rename(temporary.Name(), target); err != nil {
		return 0, fmt.Errorf("publish upload part: %w", err)
	}
	return written, nil
}

func (service *Service) finalizeCandidate(ctx context.Context, run Run, file Candidate) (bool, error) {
	parts, err := service.repository.Parts(ctx, file.ID)
	if err != nil {
		return false, fmt.Errorf("%w: read upload parts: %w", errFinalizeIO, err)
	}
	metadata, err := service.assembleFile(ctx, file, parts)
	if err != nil {
		return false, err
	}
	stopped, err := service.finalizeWrite(ctx, run, func(scope WriteScope, _ SessionState) error {
		now := service.now().UnixMilli()
		blobID, err := scope.Blobs.Ensure(ctx, metadata, now)
		if err != nil {
			return fmt.Errorf("register finalized upload: %w", err)
		}
		if err := scope.Files.Publish(
			ctx,
			FilePublication{
				Run:    run,
				FileID: file.ID,
				BlobID: blobID,
				AtMS:   now,
			},
		); err != nil {
			return fmt.Errorf("publish upload file: %w", err)
		}
		return scope.Parts.DeleteForFile(ctx, file.ID)
	})
	if err != nil {
		return false, fmt.Errorf("%w: %w", errFinalizeIO, err)
	}
	if !stopped {
		cleanup.RemoveAll(filepath.Join(service.dataDir, "tmp", "uploads", run.UploadID, file.ID))
	}
	return stopped, nil
}

func (service *Service) assembleFile(ctx context.Context, file Candidate, parts []Part) (blobstore.Metadata, error) {
	sort.Slice(parts, func(i, j int) bool { return parts[i].Offset < parts[j].Offset })
	var offset int64
	var handles []*os.File
	var readers []io.Reader
	defer func() {
		for _, handle := range handles {
			cleanup.Error("close upload part", handle.Close())
		}
	}()
	for _, part := range parts {
		if part.Offset != offset || part.Size != min(PartSize, file.Size-offset) || part.Size <= 0 {
			return blobstore.Metadata{}, errPartMissing
		}
		handle, err := os.Open(filepath.Join(service.dataDir, "tmp", "uploads", filepath.FromSlash(part.Path)))
		if err != nil {
			return blobstore.Metadata{}, fmt.Errorf("%w: %w", errPartMissing, err)
		}
		handles = append(handles, handle)
		readers = append(
			readers,
			&partReader{
				ctx: ctx,
				reader: io.LimitReader(
					handle,
					part.Size+1,
				),
				hash:     sha256.New(),
				expected: part,
			},
		)
		offset += part.Size
	}
	if offset != file.Size {
		return blobstore.Metadata{}, errPartMissing
	}
	metadata, err := service.blobs.Put(io.MultiReader(readers...))
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("%w: %w", errFinalizeIO, err)
	}
	if metadata.Size != file.Size {
		return blobstore.Metadata{}, errPartCorrupt
	}
	return metadata, nil
}

type partReader struct {
	ctx      context.Context
	reader   io.Reader
	hash     hash.Hash
	expected Part
	read     int64
}

func (reader *partReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, fmt.Errorf("read upload part: %w", err)
	}
	count, err := reader.reader.Read(buffer)
	reader.read += int64(count)
	_, _ = reader.hash.Write(buffer[:count])
	if reader.read > reader.expected.Size {
		return count, errPartCorrupt
	}
	if errors.Is(err, io.EOF) && (reader.read != reader.expected.Size || hex.EncodeToString(
		reader.hash.Sum(
			nil,
		),
	) != reader.expected.SHA256) {
		return count, errPartCorrupt
	}
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("read staged upload part: %w", err)
	}
	return count, nil
}
