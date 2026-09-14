package libraryimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/engine/rpgmaker/detector"
	"retrom/internal/capability/engine/rpgmaker/materializer"
)

// ImportArtifactBlobs stores immutable bytes during preparation, before a writer is acquired.
type ImportArtifactBlobs interface {
	OpenDigest(string) (io.ReadCloser, error)
	Put(io.Reader) (blobstore.Metadata, error)
}

type ImportArtifacts struct{ blobs ImportArtifactBlobs }

func NewImportArtifacts(blobs ImportArtifactBlobs) *ImportArtifacts {
	return &ImportArtifacts{blobs: blobs}
}

func (service *ImportArtifacts) Prepare(
	ctx context.Context, groups []PreparedGroup, archives []PreparedArchive,
) ([]PreparedGroup, error) {
	result := slices.Clone(groups)
	for index := range result {
		group := &result[index]
		group.ValidationFiles = slices.Clone(group.ValidationFiles)
		if err := service.prepareRPG(ctx, group, archives); err != nil {
			return nil, fmt.Errorf("prepare import artifacts: %w", err)
		}
	}
	return result, nil
}

func (service *ImportArtifacts) prepareRPG(
	ctx context.Context, group *PreparedGroup, archives []PreparedArchive,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("prepare RPG artifact: %w", err)
	}
	if group.RPGProfile == nil {
		return nil
	}
	role, name, err := rpgArtifactIdentity(group.RPGProfile.ExpectedGeneration)
	if err != nil {
		return err
	}
	if role == "" {
		return nil
	}
	if service.blobs == nil {
		return ErrInvalid
	}
	sources, err := service.rpgSources(ctx, group.Sources, archives)
	if err != nil {
		return err
	}
	metadata, err := service.buildRPGArtifact(role, sources)
	if err != nil {
		return fmt.Errorf("build RPG import artifact: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("complete RPG artifact: %w", err)
	}
	group.ValidationFiles = append(group.ValidationFiles, PreparedValidationFile{
		Role: role, LogicalName: name, SortOrder: len(group.ValidationFiles), Artifact: &metadata,
	})
	return nil
}

func (service *ImportArtifacts) rpgSources(
	ctx context.Context, sources []PreparedSource, archives []PreparedArchive,
) ([]materializer.SourceFile, error) {
	result := make([]materializer.SourceFile, 0, len(sources))
	for _, source := range sources {
		digest, size, err := preparedSourceIdentity(source, archives)
		if err != nil {
			return nil, err
		}
		result = append(result, materializer.SourceFile{
			Path: source.LogicalName, Size: size,
			Open: func() (io.ReadCloser, error) {
				if err := ctx.Err(); err != nil {
					return nil, fmt.Errorf("open RPG source: %w", err)
				}
				file, err := service.blobs.OpenDigest(digest)
				if err != nil {
					return nil, fmt.Errorf("read RPG source: %w", err)
				}
				return file, nil
			},
		})
	}
	return result, nil
}

func preparedSourceIdentity(source PreparedSource, archives []PreparedArchive) (string, int64, error) {
	if source.ArchiveOrdinal == nil {
		if source.File.SHA256 == "" || source.File.Size < 0 {
			return "", 0, ErrInvalid
		}
		return source.File.SHA256, source.File.Size, nil
	}
	for _, archive := range archives {
		if archive.BlobID != source.ArchiveBlobID {
			continue
		}
		metadata, present := archive.Materialized[*source.ArchiveOrdinal]
		if !present || metadata.SHA256 == "" || metadata.Size < 0 {
			return "", 0, ErrInvalid
		}
		return metadata.SHA256, metadata.Size, nil
	}
	return "", 0, ErrInvalid
}

type preparedMKXPZResult struct {
	result materializer.Result
	err    error
}

func (service *ImportArtifacts) writeMKXPZ(sources []materializer.SourceFile) (blobstore.Metadata, error) {
	reader, writer := io.Pipe()
	completed := make(chan preparedMKXPZResult, 1)
	go func() {
		result, err := materializer.WriteMKXPZ(writer, sources)
		closeErr := writer.CloseWithError(err)
		completed <- preparedMKXPZResult{result: result, err: errors.Join(err, closeErr)}
	}()
	metadata, putErr := service.blobs.Put(reader)
	closeErr := reader.CloseWithError(putErr)
	build := <-completed
	if err := errors.Join(putErr, closeErr, build.err); err != nil {
		return blobstore.Metadata{}, fmt.Errorf("materialize RPG MKXPZ: %w", err)
	}
	if metadata.SHA256 != build.result.SHA256 || metadata.Size != build.result.SizeBytes {
		return blobstore.Metadata{}, ErrInvalid
	}
	return metadata, nil
}

func rpgArtifactIdentity(generation detector.Generation) (string, string, error) {
	switch generation {
	case detector.RPG2000, detector.RPG2003:
		return "RPG_EASYRPG_INDEX", "index.json", nil
	case detector.RPGXP, detector.RPGVX, detector.RPGVXAce:
		return "RPG_MAKER_LAUNCH_BUNDLE", "game.mkxpz", nil
	case detector.RPGMV, detector.RPGMZ:
		return "", "", nil
	default:
		return "", "", ErrInvalid
	}
}

func (service *ImportArtifacts) buildRPGArtifact(
	role string, sources []materializer.SourceFile,
) (blobstore.Metadata, error) {
	if role != "RPG_EASYRPG_INDEX" {
		return service.writeMKXPZ(sources)
	}
	index, err := materializer.BuildEasyRPGIndex(sources)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("build EasyRPG index: %w", err)
	}
	metadata, err := service.blobs.Put(bytes.NewReader(index.Contents))
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("store EasyRPG index: %w", err)
	}
	return metadata, nil
}
