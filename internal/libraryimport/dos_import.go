package libraryimport

import (
	"context"

	application "retrom/internal/service/libraryimport"
)

func archiveReason(err error) string { return application.ArchiveReason(err) }
func dosProgram(path string) (string, bool) {
	return application.DOSProgram(path)
}

func rankDOSEntries(entries []preparedDOSEntry) {
	application.RankDOSEntries(entries)
}

func directDOSPathSafe(path string) bool {
	return application.DirectDOSPathSafe(path)
}

func (service *Service) prepareDOSFiles(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	return service.importPreparation().PrepareDOSFiles(ctx, sourceType, files)
}

var (
	ErrInvalid                        = application.ErrInvalid
	ErrVersionConflict                = application.ErrVersionConflict
	ErrReimportRequiredPlatformChange = application.ErrReimportRequiredPlatformChange
	ErrMultiDiscModeUnavailable       = application.ErrMultiDiscModeUnavailable
	ErrMultiDiscPlaylistMissing       = application.ErrMultiDiscPlaylistMissing
)

type CreateRequest = application.ImportRequest

type ReconfigureRequest struct {
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	MetadataProvider         string   `json:"metadataProvider"`
	TagIDs                   []string `json:"tagIds"`
}

type Created = application.ServerCreated

type initialImportProgress struct {
	state              string
	itemState          string
	runningItems       int
	reviewPendingItems int
	completed          bool
}

func newInitialImportProgress(metadataProvider string, itemCount, rejectedFileCount int) initialImportProgress {
	if itemCount == 0 {
		if rejectedFileCount > 0 {
			return initialImportProgress{state: "PARTIAL_FAILURE", itemState: "REVIEW_PENDING"}
		}
		return initialImportProgress{state: "COMPLETED", itemState: "REVIEW_PENDING", completed: true}
	}
	if metadataProvider == "HASHEOUS" {
		return initialImportProgress{
			state: "RUNNING", itemState: "SCRAPING", runningItems: itemCount,
		}
	}
	state := "REVIEW_PENDING"
	if rejectedFileCount > 0 {
		state = "PARTIAL_FAILURE"
	}
	return initialImportProgress{
		state: state, itemState: "REVIEW_PENDING", reviewPendingItems: itemCount,
	}
}

type importSourceFile = application.ImportFile

type preparedDisposition = application.PreparedDisposition

type preparedSource = application.PreparedSource

type preparedArchive = application.PreparedArchive

type preparedGroup = application.PreparedGroup

type preparedValidationFile = application.PreparedValidationFile

type preparedDOSEntry = application.PreparedDOSEntry

type reconfigurationInput struct {
	sourceImportJobID string
	sourceVersion     int64
	sourceFileIDs     []string
}

type reusableUploadFile = application.PreparedReusableUploadFile

const maxDOSBatchInspectionBytes = application.MaxDOSBatchInspectionBytes
