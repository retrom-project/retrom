package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"

	"retrom/internal/foundation/cleanup"
)

func (service *Service) stageUploadPart(
	uploadID, fileID string,
	number int,
	span byteRange,
	expected string,
	body io.Reader,
) (int64, error) {
	temporary, err := service.source.Create(uploadID, fileID)
	if err != nil {
		return 0, finalizationError("create staged upload part", err)
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

	if err := temporary.Publish(strconv.Itoa(number) + "-" + expected); err != nil {
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
		cleanup.Error("remove finalized upload parts", service.cleanupUpload(ctx, run.UploadID, file.ID))
	}
	return stopped, nil
}
