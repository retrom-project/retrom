package libraryimport

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/security/authn"
	"retrom/internal/service/tagging"
)

type ImportAdmissions struct {
	repository ImportAdmissionRepository
	notifier   ImportGroupNotifier
	tags       *tagging.Service
	options    ImportAdmissionOptions
	newID      func() (string, error)
}

func NewImportAdmissions(
	repository ImportAdmissionRepository, notifier ImportGroupNotifier,
	tags *tagging.Service, options ImportAdmissionOptions,
) *ImportAdmissions {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &ImportAdmissions{
		repository: repository, notifier: notifier, tags: tags, options: options, newID: newImportAdmissionID,
	}
}

func newImportAdmissionID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("allocate import admission identity: %w", err)
	}
	return id.String(), nil
}

func (service *ImportAdmissions) Queue(ctx context.Context, raw ImportRequest) (ServerCreated, error) {
	request, mode, err := NormalizeImportRequest(raw)
	if err != nil {
		return ServerCreated{}, err
	}
	var result ServerCreated
	err = service.repository.WithAdmission(ctx, func(scope ImportAdmissionScope) error {
		change, err := service.prepare(ctx, scope, request, mode)
		if err != nil {
			return err
		}
		if err := scope.Writer.Create(ctx, change); err != nil {
			return fmt.Errorf("persist admitted import: %w", err)
		}
		result = ServerCreated{ImportJobID: change.ImportID, JobID: change.JobID, State: "QUEUED"}
		return nil
	})
	if err != nil {
		return ServerCreated{}, fmt.Errorf("admit import: %w", err)
	}
	if service.notifier != nil {
		service.notifier.NotifyImportGroup(ctx, result.JobID)
	}
	return result, nil
}

func (service *ImportAdmissions) prepare(
	ctx context.Context, scope ImportAdmissionScope, request ImportRequest, mode string,
) (ImportAdmissionChange, error) {
	change, err := readAdmissionFacts(ctx, scope.Facts, request)
	if err != nil {
		return ImportAdmissionChange{}, err
	}
	request, mode, err = service.checkContent(request, mode, change)
	if err != nil {
		return ImportAdmissionChange{}, err
	}
	snapshot, provisional, err := SnapshotImportTarget(ctx, scope.Facts, change.Target)
	if err != nil {
		return ImportAdmissionChange{}, err
	}
	principal, _ := authn.PrincipalFromContext(ctx)
	if len(request.TagIDs) > 0 && principal.UserID == "" {
		return ImportAdmissionChange{}, ErrInvalid
	}
	tags, err := service.tags.ValidateReferences(ctx, scope.Tags, request.TagIDs)
	if err != nil {
		return ImportAdmissionChange{}, fmt.Errorf("validate admission tags: %w", err)
	}
	change.Request, change.ContentMode = request, mode
	change.Target, change.TargetSnapshot = provisional, snapshot
	change.ActorUserID, change.NowMS = principal.UserID, service.options.Now().UnixMilli()
	if err := service.identify(&change); err != nil {
		return ImportAdmissionChange{}, err
	}
	documents, err := admissionDocuments(change, tags)
	if err != nil {
		return ImportAdmissionChange{}, err
	}
	change.Documents = documents
	return change, nil
}

func readAdmissionFacts(
	ctx context.Context, facts ImportFactsReader, request ImportRequest,
) (ImportAdmissionChange, error) {
	upload, found, err := facts.Upload(ctx, request.UploadID)
	if err != nil {
		return ImportAdmissionChange{}, fmt.Errorf("read admission upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" || upload.Version < 1 {
		return ImportAdmissionChange{}, ErrInvalid
	}
	target, err := ReadImportTarget(ctx, facts, request.TargetPlatformInstanceID)
	if err != nil {
		return ImportAdmissionChange{}, err
	}
	files, err := facts.Files(ctx, request.UploadID)
	if err != nil {
		return ImportAdmissionChange{}, fmt.Errorf("read admission source files: %w", err)
	}
	if len(files) == 0 || int64(len(files)) != upload.FileCount {
		return ImportAdmissionChange{}, ErrInvalid
	}
	return ImportAdmissionChange{Request: request, Upload: upload, Target: target, Files: files}, nil
}

func (service *ImportAdmissions) checkContent(
	request ImportRequest, mode string, change ImportAdmissionChange,
) (ImportRequest, string, error) {
	return checkImportContent(request, mode, change, service.options)
}

func checkImportContent(
	request ImportRequest, mode string, change ImportAdmissionChange, options ImportAdmissionOptions,
) (ImportRequest, string, error) {
	request, mode, err := NormalizeTargetImport(
		request, mode, change.Upload.Purpose, change.Upload.SourceType, change.Files, change.Target,
	)
	if err != nil {
		return ImportRequest{}, "", err
	}
	if request.MetadataProvider == "HASHEOUS" && !options.MetadataScraperAvailable {
		return ImportRequest{}, "", ErrMetadataScraperNotConfigured
	}
	if err := ValidateImportUpload(mode, change.Upload.SourceType, change.Upload.Purpose); err != nil {
		return ImportRequest{}, "", err
	}
	capabilities := contentcapability.Resolve(
		change.Target.PlatformID, true, options.MultiDiscEnabled, change.Target.Policy,
	)
	if mode == contentcapability.ModeMultiDisc && capabilities.MultiDisc == nil {
		return ImportRequest{}, "", ErrMultiDiscModeUnavailable
	}
	return request, mode, nil
}

func (service *ImportAdmissions) identify(change *ImportAdmissionChange) error {
	for _, destination := range []*string{&change.ImportID, &change.JobID, &change.ExecutionID, &change.ConsumptionID} {
		value, err := service.newID()
		if err != nil {
			return fmt.Errorf("allocate admission records: %w", err)
		}
		*destination = value
	}
	return nil
}
