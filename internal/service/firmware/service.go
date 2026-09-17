package firmware

import (
	"context"
	"fmt"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/format/importing"
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

type preparedInstall struct {
	sourceKind string
	fileKind   string
	blobID     string
	sha256     string
	entries    []importing.ArchiveEntry
}

func (service *Service) prepareInstall(
	ctx context.Context,
	id string,
	version int64,
	fileID string,
) (preparedInstall, error) {
	var prepared preparedInstall
	err := service.repository.WithRead(ctx, func(scope ReadScope) error {
		requirement, found, err := scope.Requirements.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("read BIOS requirement: %w", err)
		}
		if !found || !requirement.Enabled || requirement.Version != version {
			return ErrInvalid
		}
		upload, found, err := scope.Uploads.Get(ctx, fileID)
		if err != nil {
			return fmt.Errorf("read BIOS upload: %w", err)
		}
		if !found || upload.State != "COMPLETE" {
			return ErrInvalid
		}
		prepared.sourceKind = requirement.SourceKind
		prepared.fileKind = requirement.FileKind
		prepared.blobID = upload.BlobID
		prepared.sha256 = upload.SHA256
		return nil
	})
	if err != nil {
		return preparedInstall{}, fmt.Errorf("prepare BIOS installation: %w", err)
	}
	if prepared.fileKind == "ARCHIVE" {
		if service.blobs == nil {
			return preparedInstall{}, ErrInvalid
		}
		entries, err := importing.ScanZIP(
			ctx,
			service.blobs.Path(prepared.sha256),
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
	result, err := service.repository.CommitBrowserInstall(ctx, BrowserInstallCommand{
		RequirementID:      id,
		FileID:             request.UploadFileID,
		Version:            version,
		PreparedSourceKind: prepared.sourceKind,
		PreparedFileKind:   prepared.fileKind,
		PreparedBlobID:     prepared.blobID,
		PreparedSHA256:     prepared.sha256,
		ArchiveEntries:     prepared.entries,
		NowMS:              service.now().UnixMilli(),
	})
	if err != nil {
		return Installation{}, fmt.Errorf("install BIOS: %w", err)
	}
	service.signalRelease()
	return result, nil
}

func (service *Service) signalRelease() {
	if service.releases != nil {
		service.releases.Signal()
	}
}
