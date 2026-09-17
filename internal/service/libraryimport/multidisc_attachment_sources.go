package libraryimport

import (
	"context"
	"fmt"
	model "retrom/internal/model/libraryimport"
)

type MultiDiscAttachmentSources struct {
	repository model.MultiDiscAttachmentSourceRepository
}

func NewMultiDiscAttachmentSources(repository model.MultiDiscAttachmentSourceRepository) *MultiDiscAttachmentSources {
	return &MultiDiscAttachmentSources{repository: repository}
}

func (service *MultiDiscAttachmentSources) BaseFiles(
	ctx context.Context, snapshotID string,
) (model.MultiDiscAttachmentBaseFiles, error) {
	if snapshotID == "" {
		return model.MultiDiscAttachmentBaseFiles{}, model.ErrInvalid
	}
	result, err := service.repository.BaseFiles(ctx, snapshotID)
	if err != nil {
		return model.MultiDiscAttachmentBaseFiles{}, fmt.Errorf("read multi-disc base files: %w", err)
	}
	return result, nil
}

func (service *MultiDiscAttachmentSources) UploadFiles(
	ctx context.Context, sessionID string,
) (model.MultiDiscAttachmentUploadFiles, error) {
	if sessionID == "" {
		return model.MultiDiscAttachmentUploadFiles{}, model.ErrInvalid
	}
	result, err := service.repository.UploadFiles(ctx, sessionID)
	if err != nil {
		return model.MultiDiscAttachmentUploadFiles{}, fmt.Errorf("read multi-disc upload files: %w", err)
	}
	return result, nil
}
