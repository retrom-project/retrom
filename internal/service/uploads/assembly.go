package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"sort"

	model "retrom/internal/model/uploads"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/uploadfiles"
)

func (service *Service) assembleFile(
	ctx context.Context,
	file model.Candidate,
	parts []model.Part,
) (blobstore.Metadata, error) {
	sort.Slice(parts, func(i, j int) bool { return parts[i].Offset < parts[j].Offset })
	var offset int64
	for _, part := range parts {
		if part.Offset != offset || part.Size != min(model.PartSize, file.Size-offset) || part.Size <= 0 {
			return blobstore.Metadata{}, &model.BrokenPart{
				FileID: file.ID, Number: int(offset / model.PartSize), Missing: true, Cause: errPartMissing,
			}
		}
		offset += part.Size
	}
	if offset != file.Size {
		return blobstore.Metadata{}, &model.BrokenPart{
			FileID: file.ID, Number: int(offset / model.PartSize), Missing: true, Cause: errPartMissing,
		}
	}
	reader := &assemblyReader{ctx: ctx, source: service.source, fileID: file.ID, parts: parts}
	metadata, err := service.blobs.Put(reader)
	err = errors.Join(err, reader.Close())
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("%w: %w", errFinalizeIO, err)
	}
	if metadata.Size != file.Size {
		return blobstore.Metadata{}, fmt.Errorf("%w: assembled size mismatch", errFinalizeIO)
	}
	return metadata, nil
}

type assemblyReader struct {
	ctx    context.Context
	source *uploadfiles.Store
	fileID string
	parts  []model.Part
	index  int
	handle io.ReadCloser
	reader *partReader
}

func (reader *assemblyReader) Close() error {
	if reader.handle == nil {
		return nil
	}
	err := reader.handle.Close()
	reader.handle = nil
	if err != nil {
		return fmt.Errorf("close staged upload part: %w", err)
	}
	return nil
}

func (reader *assemblyReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, finalizationError("read upload bytes", err)
	}
	for reader.index < len(reader.parts) {
		part := reader.parts[reader.index]
		if reader.handle == nil {
			handle, err := reader.source.Open(part.Path)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return 0, &model.BrokenPart{
						FileID: reader.fileID, Number: part.Number, Part: part, Cause: errors.Join(errPartMissing, err),
					}
				}
				return 0, finalizationError("read upload bytes", err)
			}
			reader.handle = handle
			reader.reader = &partReader{
				ctx: reader.ctx, reader: io.LimitReader(handle, part.Size+1), hash: sha256.New(), expected: part,
			}
		}
		count, err := reader.reader.Read(buffer)
		if errors.Is(err, errPartCorrupt) {
			return count, &model.BrokenPart{FileID: reader.fileID, Number: part.Number, Part: part, Cause: err}
		}
		if !errors.Is(err, io.EOF) {
			return count, err
		}
		if err := reader.Close(); err != nil {
			return count, err
		}
		reader.index++
		if count > 0 {
			return count, nil
		}
	}
	return 0, io.EOF
}

type partReader struct {
	ctx      context.Context
	reader   io.Reader
	hash     hash.Hash
	expected model.Part
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
	if errors.Is(err, io.EOF) && (reader.read != reader.expected.Size ||
		hex.EncodeToString(reader.hash.Sum(nil)) != reader.expected.SHA256) {
		return count, errPartCorrupt
	}
	if err == io.EOF {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("read staged upload part: %w", err)
	}
	return count, nil
}
