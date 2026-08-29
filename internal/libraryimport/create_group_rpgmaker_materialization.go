package libraryimport

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"retrom/internal/blobstore"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/materializer"
)

func (run *creationRun) prepareRPGMakerValidationFiles(record *groupRecord) error {
	profile := record.group.rpgProfile
	if profile == nil {
		return nil
	}
	sources, err := run.rpgMakerMaterializerSources(record)
	if err != nil {
		return err
	}
	switch profile.ExpectedGeneration {
	case detector.RPG2000, detector.RPG2003:
		index, buildErr := materializer.BuildEasyRPGIndex(sources)
		if buildErr != nil {
			return fmt.Errorf("libraryimport/rpgmaker index: %w", buildErr)
		}
		metadata, putErr := run.service.blobs.Put(bytes.NewReader(index.Contents))
		if putErr != nil {
			return fmt.Errorf("libraryimport/rpgmaker index: %w", putErr)
		}
		return run.appendRPGValidationBlob(record, "RPG_EASYRPG_INDEX", "index.json", metadata)
	case detector.RPGXP, detector.RPGVX, detector.RPGVXAce:
		metadata, buildErr := run.writeRPGMakerMKXPZ(sources)
		if buildErr != nil {
			return buildErr
		}
		return run.appendRPGValidationBlob(record, "RPG_MAKER_LAUNCH_BUNDLE", "game.mkxpz", metadata)
	case detector.RPGMV, detector.RPGMZ:
		return nil
	default:
		return ErrInvalid
	}
}

func (run *creationRun) rpgMakerMaterializerSources(record *groupRecord) ([]materializer.SourceFile, error) {
	result := make([]materializer.SourceFile, 0, len(record.group.sources))
	for _, source := range record.group.sources {
		blobID := run.sourceBlobID(source)
		var digest string
		var size int64
		if err := run.transaction.QueryRowContext(run.ctx, `
SELECT sha256,size_bytes FROM blobs WHERE id=?
`, blobID).Scan(&digest, &size); err != nil {
			return nil, fmt.Errorf("libraryimport/rpgmaker materializer: %w", err)
		}
		digestCopy := digest
		result = append(result, materializer.SourceFile{
			Path: source.logicalName, Size: size,
			Open: func() (io.ReadCloser, error) { return run.service.blobs.OpenDigest(digestCopy) },
		})
	}
	return result, nil
}

type mkxpBuildResult struct {
	result materializer.Result
	err    error
}

func (run *creationRun) writeRPGMakerMKXPZ(sources []materializer.SourceFile) (blobstore.Metadata, error) {
	reader, writer := io.Pipe()
	resultChannel := make(chan mkxpBuildResult, 1)
	go func() {
		result, err := materializer.WriteMKXPZ(writer, sources)
		if err != nil {
			_ = writer.CloseWithError(err)
		} else {
			err = writer.Close()
		}
		resultChannel <- mkxpBuildResult{result: result, err: err}
	}()
	metadata, putErr := run.service.blobs.Put(reader)
	build := <-resultChannel
	if putErr != nil || build.err != nil {
		return blobstore.Metadata{}, fmt.Errorf("libraryimport/rpgmaker mkxpz: %w", errors.Join(putErr, build.err))
	}
	if metadata.SHA256 != build.result.SHA256 || metadata.Size != build.result.SizeBytes {
		return blobstore.Metadata{}, ErrInvalid
	}
	return metadata, nil
}

func (run *creationRun) appendRPGValidationBlob(
	record *groupRecord,
	role string,
	logicalName string,
	metadata blobstore.Metadata,
) error {
	blobID, err := blobstore.EnsureRecord(
		run.ctx, run.transaction, metadata, "application/octet-stream", run.now,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/rpgmaker materializer: %w", err)
	}
	record.group.validationFiles = append(record.group.validationFiles, preparedValidationFile{
		role: role, logicalName: logicalName, blobID: blobID, sortOrder: len(record.group.validationFiles),
	})
	return nil
}
