package libraryimport

import (
	"context"
	libraryimportmodel "retrom/internal/model/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
)

func archiveReason(err error) string { return libraryimportservice.ArchiveReason(err) }
func dosProgram(path string) (string, bool) {
	return libraryimportservice.DOSProgram(path)
}

func rankDOSEntries(entries []preparedDOSEntry) {
	libraryimportservice.RankDOSEntries(entries)
}

func directDOSPathSafe(path string) bool {
	return libraryimportservice.DirectDOSPathSafe(path)
}

func (service *Service) prepareDOSFiles(
	ctx context.Context,
	sourceType string,
	files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive) {
	return service.importPreparation().PrepareDOSFiles(ctx, sourceType, files)
}

var (
	ErrInvalid                        = libraryimportmodel.ErrInvalid
	ErrVersionConflict                = libraryimportmodel.ErrVersionConflict
	ErrReimportRequiredPlatformChange = libraryimportmodel.ErrReimportRequiredPlatformChange
	ErrMultiDiscModeUnavailable       = libraryimportmodel.ErrMultiDiscModeUnavailable
	ErrMultiDiscPlaylistMissing       = libraryimportmodel.ErrMultiDiscPlaylistMissing
)

type CreateRequest = libraryimportmodel.ImportRequest

type ReconfigureRequest struct {
	TargetPlatformInstanceID string   `json:"targetPlatformInstanceId"`
	MetadataProvider         string   `json:"metadataProvider"`
	TagIDs                   []string `json:"tagIds"`
}

type Created = libraryimportmodel.ServerCreated

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

type importSourceFile = libraryimportmodel.ImportFile

type preparedDisposition = libraryimportmodel.PreparedDisposition

type preparedSource = libraryimportmodel.PreparedSource

type preparedArchive = libraryimportmodel.PreparedArchive

type preparedGroup = libraryimportmodel.PreparedGroup

type preparedValidationFile = libraryimportmodel.PreparedValidationFile

type preparedDOSEntry = libraryimportmodel.PreparedDOSEntry

type reconfigurationInput struct {
	sourceImportJobID string
	sourceVersion     int64
	sourceFileIDs     []string
}

type reusableUploadFile = libraryimportmodel.PreparedReusableUploadFile

const maxDOSBatchInspectionBytes = libraryimportservice.MaxDOSBatchInspectionBytes
