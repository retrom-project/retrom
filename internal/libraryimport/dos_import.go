package libraryimport

import (
	"context"

	libraryservice "retrom/internal/service/libraryimport"
)

func archiveReason(err error) string { return libraryservice.ArchiveReason(err) }
func dosProgram(path string) (string, bool) {
	return libraryservice.DOSProgram(path)
}

func rankDOSEntries(entries []preparedDOSEntry) {
	libraryservice.RankDOSEntries(entries)
}

func directDOSPathSafe(path string) bool {
	return libraryservice.DirectDOSPathSafe(path)
}

func (service *Service) prepareDOSFiles(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	return service.preparation.PrepareDOSFiles(ctx, sourceType, files)
}

var (
	ErrInvalid                        = libraryservice.ErrInvalid
	ErrVersionConflict                = libraryservice.ErrVersionConflict
	ErrReimportRequiredPlatformChange = libraryservice.ErrReimportRequiredPlatformChange
	ErrMultiDiscModeUnavailable       = libraryservice.ErrMultiDiscModeUnavailable
	ErrMultiDiscPlaylistMissing       = libraryservice.ErrMultiDiscPlaylistMissing
)

type CreateRequest = libraryservice.ImportRequest

type ReconfigureRequest struct {
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	MetadataProvider         string   `json:"metadataProvider"`
	TagIDs                   []string `json:"tagIds"`
}

type Created = libraryservice.ServerCreated

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

type importSourceFile = libraryservice.ImportFile

type preparedDisposition = libraryservice.PreparedDisposition

type preparedSource = libraryservice.PreparedSource

type preparedArchive = libraryservice.PreparedArchive

type preparedGroup = libraryservice.PreparedGroup

type preparedValidationFile = libraryservice.PreparedValidationFile

type preparedDOSEntry = libraryservice.PreparedDOSEntry

type reconfigurationInput struct {
	sourceImportJobID string
	sourceVersion     int64
	sourceFileIDs     []string
}

type reusableUploadFile = libraryservice.PreparedReusableUploadFile

const maxDOSBatchInspectionBytes = libraryservice.MaxDOSBatchInspectionBytes
