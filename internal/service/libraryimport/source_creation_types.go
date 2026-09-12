package libraryimport

import "context"

type (
	SourceCreationIntent struct {
		ImportID, ItemID, JobID, WorkerID string
		ExecutionNo, Attempt              int64
		PrimaryPaths                      []string
	}
	OwnedServerSourceRequest struct {
		Intent                                SourceCreationIntent
		TargetPlatformInstanceID, ContentMode string
		Files                                 []ServerSourceFile
		TagIDs                                []string
		AssignedByUserID                      string
	}
	ServerSourceFile struct {
		RelativePath, BlobID string
		SizeBytes            int64
	}
	ServerCreated struct {
		ImportJobID string `json:"importJobId"`
		JobID       string `json:"jobId"`
		State       string `json:"state"`
		ItemCount   int    `json:"itemCount"`
	}
	ServerImportItem struct {
		ItemID, State, ValidationStatus, CompatibilityCode    string
		CoreID, CoreName, DependencySnapshotJSON              string
		ContentKind, SourceManifestJSON, SourceManifestDigest string
		ExistingGameID                                        string
		ExistingMatches                                       []ServerDuplicateMatch
		SourceRelativePaths                                   []string
	}
	ServerDuplicateMatch struct {
		GameID string `json:"gameId"`
	}
	ServerImportResult struct {
		Created       ServerCreated
		Items         []ServerImportItem
		RejectedCodes []string
	}
	SourceCreationFile struct {
		File               ServerSourceFile
		State, FactsDigest string
	}
	SourceCreationSnapshot struct {
		ImportID, ItemID, JobID, WorkerID                               string
		SourceState, ImportState, JobState, MappingAction               string
		SourceVersion, ImportVersion, JobVersion, ExecutionNo, Attempt  int64
		LeaseUntilMS, DeadlineMS, TargetVersion                         int64
		TargetPlatformInstanceID, TargetPlatformID, TargetDefaultCoreID string
		TargetProviderID, TargetID, TargetDATVersionID                  string
		UploadID, LibraryJobID, LibraryItemID                           string
		PrimaryPaths                                                    []string
		Files                                                           []SourceCreationFile
	}
	SourceBindingChange struct {
		Before  SourceCreationSnapshot
		Created ServerCreated
		Item    ServerImportItem
		NowMS   int64
	}
	SourceOwnershipRecords interface {
		ReadSource(context.Context, SourceCreationIntent) (SourceCreationSnapshot, error)
		BindSource(context.Context, SourceBindingChange) error
	}
	OwnedSourceLookup struct {
		Result                                                ServerImportResult
		TargetPlatformInstanceID, ContentMode, ManifestDigest string
		PrimaryPaths                                          []string
	}
)
