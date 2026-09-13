package libraryimport

import (
	"context"
	"fmt"
)

type MultiDiscAttachmentSources struct {
	repository MultiDiscAttachmentSourceRepository
}

func NewMultiDiscAttachmentSources(repository MultiDiscAttachmentSourceRepository) *MultiDiscAttachmentSources {
	return &MultiDiscAttachmentSources{repository: repository}
}

func (service *MultiDiscAttachmentSources) BaseFiles(
	ctx context.Context, snapshotID string,
) (MultiDiscAttachmentBaseFiles, error) {
	if snapshotID == "" {
		return MultiDiscAttachmentBaseFiles{}, ErrInvalid
	}
	result, err := service.repository.BaseFiles(ctx, snapshotID)
	if err != nil {
		return MultiDiscAttachmentBaseFiles{}, fmt.Errorf("read multi-disc base files: %w", err)
	}
	return result, nil
}

func (service *MultiDiscAttachmentSources) UploadFiles(
	ctx context.Context, sessionID string,
) (MultiDiscAttachmentUploadFiles, error) {
	if sessionID == "" {
		return MultiDiscAttachmentUploadFiles{}, ErrInvalid
	}
	result, err := service.repository.UploadFiles(ctx, sessionID)
	if err != nil {
		return MultiDiscAttachmentUploadFiles{}, fmt.Errorf("read multi-disc upload files: %w", err)
	}
	return result, nil
}
