package firmware

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/firmware"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/format/importing"
)

type Service struct {
	repository model.Repository
	blobs      *blobstore.Store
	releases   model.ReleaseSignal
	now        func() time.Time
}

func New(repository model.Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func (service *Service) WithPayloadRelease(releases model.ReleaseSignal) *Service {
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
	facts, err := service.repository.LoadInstallFacts(ctx, id, version, fileID)
	if err != nil {
		return preparedInstall{}, fmt.Errorf("prepare BIOS installation: %w", err)
	}
	prepared := preparedInstall{
		sourceKind: facts.SourceKind,
		fileKind:   facts.FileKind,
		blobID:     facts.BlobID,
		sha256:     facts.SHA256,
	}
	if prepared.fileKind == "ARCHIVE" {
		if service.blobs == nil {
			return preparedInstall{}, model.ErrInvalid
		}
		entries, err := importing.ScanZIP(
			ctx,
			service.blobs.Path(prepared.sha256),
			importing.DefaultArchiveLimits(),
		)
		if err != nil {
			return preparedInstall{}, fmt.Errorf("%w: inspect BIOS archive: %w", model.ErrInvalid, err)
		}
		prepared.entries = entries
	}
	return prepared, nil
}

func (service *Service) Install(
	ctx context.Context,
	id string,
	version int64,
	request model.InstallRequest,
) (model.Installation, error) {
	prepared, err := service.prepareInstall(ctx, id, version, request.UploadFileID)
	if err != nil {
		return model.Installation{}, err
	}
	result, err := service.repository.CommitBrowserInstall(ctx, model.BrowserInstallCommand{
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
		return model.Installation{}, fmt.Errorf("install BIOS: %w", err)
	}
	service.signalRelease()
	return result, nil
}

func (service *Service) signalRelease() {
	if service.releases != nil {
		service.releases.Signal()
	}
}
