package uploads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"

	model "retrom/internal/model/uploads"

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
		return 0, fmt.Errorf("%w: receive part: %w", model.ErrInvalid, copyErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("close upload part: %w", closeErr)
	}
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if written != span.end-span.start+1 || actualDigest != expected {
		return 0, model.ErrInvalid
	}

	if err := temporary.Publish(strconv.Itoa(number) + "-" + expected); err != nil {
		return 0, fmt.Errorf("publish upload part: %w", err)
	}
	return written, nil
}

func (service *Service) finalizeCandidate(ctx context.Context, run model.Run, file model.Candidate) (bool, error) {
	parts, err := service.repository.Parts(ctx, file.ID)
	if err != nil {
		return false, fmt.Errorf("%w: read upload parts: %w", errFinalizeIO, err)
	}
	metadata, err := service.assembleFile(ctx, file, parts)
	if err != nil {
		return false, err
	}
	now := service.now().UnixMilli()
	stopped, err := service.repository.CommitPublishFile(ctx, model.PublishFileCommand{
		Run: run, FileID: file.ID, Metadata: metadata, NowMS: now,
	})
	if err != nil {
		return false, fmt.Errorf("%w: write upload finalization: %w", errFinalizeIO, err)
	}
	if !stopped {
		cleanup.Error("remove finalized upload parts", service.cleanupUpload(ctx, run.UploadID, file.ID))
	}
	return stopped, nil
}
