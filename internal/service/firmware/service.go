package firmware

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"

	"github.com/google/uuid"
)

type Service struct {
	repository Repository
	blobs      *blobstore.Store
	releases   ReleaseSignal
	now        func() time.Time
}

func New(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func (service *Service) WithPayloadRelease(releases ReleaseSignal) *Service {
	service.releases = releases
	return service
}

type installSnapshot struct {
	Requirement Requirement
	Upload      Upload
}
type preparedInstall struct {
	snapshot installSnapshot
	entries  []importing.ArchiveEntry
}

func readInstallSnapshot(
	ctx context.Context,
	scope ReadScope,
	id, fileID string,
	version int64,
) (installSnapshot, error) {
	requirement, found, err := scope.Requirements.Get(ctx, id)
	if err != nil {
		return installSnapshot{}, fmt.Errorf("read BIOS requirement: %w", err)
	}
	if !found || !requirement.Enabled || requirement.Version != version {
		return installSnapshot{}, ErrInvalid
	}
	upload, found, err := scope.Uploads.Get(ctx, fileID)
	if err != nil {
		return installSnapshot{}, fmt.Errorf("read BIOS upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" {
		return installSnapshot{}, ErrInvalid
	}
	return installSnapshot{Requirement: requirement, Upload: upload}, nil
}

func (service *Service) prepareInstall(
	ctx context.Context,
	id string,
	version int64,
	fileID string,
) (preparedInstall, error) {
	var prepared preparedInstall
	err := service.repository.WithRead(ctx, func(scope ReadScope) error {
		var err error
		prepared.snapshot, err = readInstallSnapshot(ctx, scope, id, fileID, version)
		return err
	})
	if err != nil {
		return preparedInstall{}, fmt.Errorf("prepare BIOS installation: %w", err)
	}
	if prepared.snapshot.Requirement.FileKind == "ARCHIVE" {
		if service.blobs == nil {
			return preparedInstall{}, ErrInvalid
		}
		entries, err := importing.ScanZIP(
			ctx,
			service.blobs.Path(
				prepared.snapshot.Upload.SHA256,
			),
			importing.DefaultArchiveLimits(),
		)
		if err != nil {
			return preparedInstall{}, fmt.Errorf("%w: inspect BIOS archive: %w", ErrInvalid, err)
		}
		prepared.entries = entries
	}
	return prepared, nil
}

func (service *Service) Install(
	ctx context.Context,
	id string,
	version int64,
	request InstallRequest,
) (Installation, error) {
	prepared, err := service.prepareInstall(ctx, id, version, request.UploadFileID)
	if err != nil {
		return Installation{}, err
	}
	var result Installation
	err = service.repository.WithWrite(ctx, func(scope WriteScope) error {
		current, err := readInstallSnapshot(ctx, scope.ReadScope, id, request.UploadFileID, version)
		if err != nil {
			return err
		}
		if !sameInstallSource(current, prepared.snapshot) {
			return ErrInvalid
		}
		status, details, err := evaluateInstall(ctx, scope.Requirements, current, prepared.entries)
		if err != nil {
			return err
		}
		if status == "INVALID" {
			return &firmware.ArchiveContentError{Details: details}
		}
		now := service.now().UnixMilli()
		if current.Requirement.FileKind == "ARCHIVE" {
			if err := scope.Archives.Put(ctx, current.Upload.BlobID, prepared.entries, now); err != nil {
				return fmt.Errorf("record BIOS archive facts: %w", err)
			}
		}
		result, err = persistBrowserInstallation(ctx, scope, current, status, details, now)
		return err
	})
	if err != nil {
		return Installation{}, fmt.Errorf("install BIOS: %w", err)
	}
	service.signalRelease()
	return result, nil
}

func sameInstallSource(current, prepared installSnapshot) bool {
	return current.Requirement.SourceKind == prepared.Requirement.SourceKind &&
		current.Requirement.FileKind == prepared.Requirement.FileKind &&
		current.Upload.BlobID == prepared.Upload.BlobID && current.Upload.SHA256 == prepared.Upload.SHA256
}

func persistBrowserInstallation(ctx context.Context, scope WriteScope, snapshot installSnapshot, status string,
	details map[string]any, now int64,
) (Installation, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Installation{}, fmt.Errorf("generate BIOS installation ID: %w", err)
	}
	consumption, err := uuid.NewV7()
	if err != nil {
		return Installation{}, fmt.Errorf("generate BIOS consumption ID: %w", err)
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return Installation{}, fmt.Errorf("encode BIOS findings: %w", err)
	}
	requirement, upload := snapshot.Requirement, snapshot.Upload
	if err := SupersedeInScope(ctx, scope.Retirements, requirement.ID, now); err != nil {
		return Installation{}, fmt.Errorf("retire BIOS: %w", err)
	}
	if err := scope.Installations.Create(ctx, InstallationWrite{
		ID: id.String(), RequirementID: requirement.ID, BlobID: upload.BlobID, Filename: upload.RelativePath,
		MD5: upload.MD5, SHA1: upload.SHA1, SHA256: upload.SHA256, Size: upload.Size, Status: status,
		RequirementVersion: requirement.Version, DetailsJSON: encoded, AtMS: now, SourceKind: "BROWSER_UPLOAD",
	}); err != nil {
		return Installation{}, fmt.Errorf("persist BIOS installation: %w", err)
	}
	if err := scope.Installations.Consume(ctx, Consumption{
		ID: consumption.String(), UploadID: upload.SessionID,
		FileID: upload.ID, InstallationID: id.String(), AtMS: now,
	}); err != nil {
		return Installation{}, fmt.Errorf("consume BIOS upload: %w", err)
	}
	return Installation{
		InstallationID: id.String(), RequirementID: requirement.ID, Status: status, Active: true,
		ValidatedRequirementVersion: requirement.Version, ValidationDetails: details, CreatedAtMS: now,
	}, nil
}

func (service *Service) signalRelease() {
	if service.releases != nil {
		service.releases.Signal()
	}
}
