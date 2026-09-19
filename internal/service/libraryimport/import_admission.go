package libraryimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/libraryimport"

	"github.com/google/uuid"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/security/authn"
)

type ImportAdmissions struct {
	repository model.ImportAdmissionRepository
	notifier   model.ImportGroupNotifier
	options    ImportAdmissionOptions
	newID      func() (string, error)
}

func NewImportAdmissions(
	repository model.ImportAdmissionRepository, notifier model.ImportGroupNotifier,
	_ interface{}, options ImportAdmissionOptions,
) *ImportAdmissions {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &ImportAdmissions{
		repository: repository, notifier: notifier, options: options, newID: newImportAdmissionID,
	}
}

func newImportAdmissionID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("allocate import admission identity: %w", err)
	}
	return id.String(), nil
}

func (service *ImportAdmissions) Queue(ctx context.Context, raw model.ImportRequest) (model.ServerCreated, error) {
	request, mode, err := NormalizeImportRequest(raw)
	if err != nil {
		return model.ServerCreated{}, err
	}
	facts, err := service.repository.ReadAdmissionFacts(ctx, request)
	if err != nil {
		return model.ServerCreated{}, fmt.Errorf("admit import: %w", err)
	}
	change := model.ImportAdmissionChange{
		Request: request, Upload: facts.Upload, Target: facts.Target,
		TargetSnapshot: facts.TargetSnapshot, Files: facts.Files,
	}
	request, mode, err = service.checkContent(request, mode, change)
	if err != nil {
		return model.ServerCreated{}, err
	}
	principal, _ := authn.PrincipalFromContext(ctx)
	if len(request.TagIDs) > 0 && principal.UserID == "" {
		return model.ServerCreated{}, model.ErrInvalid
	}
	change.Request, change.ContentMode = request, mode
	change.ActorUserID, change.NowMS = principal.UserID, service.options.Now().UnixMilli()
	if err := service.identify(&change); err != nil {
		return model.ServerCreated{}, err
	}
	if err := service.repository.CommitAdmission(ctx, change); err != nil {
		return model.ServerCreated{}, fmt.Errorf("admit import: %w", err)
	}
	result := model.ServerCreated{ImportJobID: change.ImportID, JobID: change.JobID, State: "QUEUED"}
	if service.notifier != nil {
		service.notifier.NotifyImportGroup(ctx, result.JobID)
	}
	return result, nil
}

func (service *ImportAdmissions) checkContent(
	request model.ImportRequest, mode string, change model.ImportAdmissionChange,
) (model.ImportRequest, string, error) {
	return checkImportContent(request, mode, change, service.options)
}

func checkImportContent(
	request model.ImportRequest, mode string, change model.ImportAdmissionChange, options ImportAdmissionOptions,
) (model.ImportRequest, string, error) {
	request, mode, err := NormalizeTargetImport(
		request, mode, change.Upload.Purpose, change.Upload.SourceType, change.Files, change.Target,
	)
	if err != nil {
		return model.ImportRequest{}, "", err
	}
	if request.MetadataProvider == "HASHEOUS" && !options.MetadataScraperAvailable {
		return model.ImportRequest{}, "", ErrMetadataScraperNotConfigured
	}
	if err := ValidateImportUpload(mode, change.Upload.SourceType, change.Upload.Purpose); err != nil {
		return model.ImportRequest{}, "", err
	}
	capabilities := contentcapability.Resolve(
		change.Target.PlatformID, true, options.MultiDiscEnabled, change.Target.Policy,
	)
	if mode == contentcapability.ModeMultiDisc && capabilities.MultiDisc == nil {
		return model.ImportRequest{}, "", model.ErrMultiDiscModeUnavailable
	}
	return request, mode, nil
}

// readAdmissionFacts reads and validates the database facts needed for an
// import admission from the given reader.
func readAdmissionFacts(
	ctx context.Context, facts model.ImportFactsReader, request model.ImportRequest,
) (model.ImportAdmissionChange, error) {
	upload, found, err := facts.Upload(ctx, request.UploadID)
	if err != nil {
		return model.ImportAdmissionChange{}, fmt.Errorf("read admission upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" || upload.Version < 1 {
		return model.ImportAdmissionChange{}, model.ErrInvalid
	}
	target, err := ReadImportTarget(ctx, facts, request.TargetPlatformInstanceID)
	if err != nil {
		return model.ImportAdmissionChange{}, err
	}
	files, err := facts.Files(ctx, request.UploadID)
	if err != nil {
		return model.ImportAdmissionChange{}, fmt.Errorf("read admission source files: %w", err)
	}
	if len(files) == 0 || int64(len(files)) != upload.FileCount {
		return model.ImportAdmissionChange{}, model.ErrInvalid
	}
	return model.ImportAdmissionChange{Request: request, Upload: upload, Target: target, Files: files}, nil
}

func (service *ImportAdmissions) identify(change *model.ImportAdmissionChange) error {
	for _, destination := range []*string{&change.ImportID, &change.JobID, &change.ExecutionID, &change.ConsumptionID} {
		value, err := service.newID()
		if err != nil {
			return fmt.Errorf("allocate admission records: %w", err)
		}
		*destination = value
	}
	return nil
}

