package libraryimport

import (
	"context"
	"math"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/security/authn"
)

type MultiDiscAttachments struct {
	repository model.MultiDiscAttachmentRepository
	options    MultiDiscAttachmentOptions
	newID      func() (string, error)
}

func NewMultiDiscAttachments(
	repository model.MultiDiscAttachmentRepository, options MultiDiscAttachmentOptions,
) *MultiDiscAttachments {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &MultiDiscAttachments{repository: repository, options: options, newID: newImportAdmissionID}
}

func (service *MultiDiscAttachments) Create(
	ctx context.Context,
	itemID string,
	version int64,
	request model.MultiDiscAttachmentRequest,
) (model.MultiDiscAttachmentCreated, error) {
	principal, authenticated := authn.PrincipalFromContext(ctx)
	if version < 1 || version == math.MaxInt64 || itemID == "" || request.UploadID == "" ||
		!service.options.StorageAvailable || !authenticated || principal.UserID == "" {
		return model.MultiDiscAttachmentCreated{}, multiDiscAttachmentError(
			model.MultiDiscAttachmentErrorInvalid,
			model.ErrInvalid,
		)
	}
	write := model.MultiDiscAttachmentWrite{RequestVersion: version}
	for _, destination := range []*string{&write.Input.AttachmentID, &write.JobID, &write.AuditID} {
		id, err := service.newID()
		if err != nil {
			return model.MultiDiscAttachmentCreated{}, multiDiscAttachmentError(
				model.MultiDiscAttachmentErrorUnavailable,
				err,
			)
		}
		if id == "" {
			return model.MultiDiscAttachmentCreated{}, multiDiscAttachmentError(
				model.MultiDiscAttachmentErrorUnavailable,
				model.ErrInvalid,
			)
		}
		*destination = id
	}
	cmd := model.AttachmentAdmissionCommand{
		ItemID:           itemID,
		ExpectedVersion:  version,
		UploadID:         request.UploadID,
		ActorUserID:      principal.UserID,
		StorageAvailable: service.options.StorageAvailable,
		NowMS:            service.options.Now().UnixMilli(),
		AttachmentID:     write.Input.AttachmentID,
		JobID:            write.JobID,
		AuditID:          write.AuditID,
	}
	result, err := service.repository.CommitAttachmentAdmission(ctx, cmd)
	if err != nil {
		return model.MultiDiscAttachmentCreated{}, multiDiscAttachmentError(multiDiscAdmissionErrorCode(err), err)
	}
	return result, nil
}

func multiDiscAdmissionErrorCode(err error) string {
	if code := model.MultiDiscAttachmentErrorCode(err); code != "" {
		return code
	}
	return model.MultiDiscAttachmentErrorUnavailable
}
