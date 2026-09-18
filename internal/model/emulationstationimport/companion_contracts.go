package emulationstationimport

import "context"

type CompanionOwner struct {
	Before         OwnedItem
	Mapping        MappingTarget
	CollectionID   string
	MappingVersion int64
}

type CompanionFile struct {
	ItemID, CollectionID, Path, Facts string
	Ordinal, Size                     int64
}

type CompanionSelection struct {
	Owner CompanionOwner
	Files []CompanionFile
}

type CompanionBinding struct {
	Before CompanionOwner
	File   CompanionFile
	Blob   VerifiedBlob
	NowMS  int64
}

type CompanionRepository interface {
	LoadCompanionOwner(context.Context, string) (CompanionOwner, error)
	LoadMappingTarget(context.Context, string) (MappingTarget, bool, error)
	LoadDependencies(context.Context, string, string) ([]string, error)
	LoadCandidates(context.Context, CompanionOwner) ([]CompanionFile, error)
	CommitCompanionBinding(context.Context, CompanionBinding) (string, error)
}

type CompanionSources interface {
	CopyFile(context.Context, Execution, ExecutionFile) (VerifiedBlob, error)
}
