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

type CompanionReader interface {
	Owner(context.Context, string) (CompanionOwner, error)
	Target(context.Context, string) (MappingTarget, bool, error)
	Dependencies(context.Context, string, string) ([]string, error)
	Candidates(context.Context, CompanionOwner) ([]CompanionFile, error)
}

type CompanionWriter interface {
	Register(context.Context, CompanionBinding) (string, error)
}

type CompanionScope struct {
	Read  CompanionReader
	Write CompanionWriter
}

type CompanionRepository interface {
	WithCompanions(context.Context, func(CompanionScope) error) error
}

type CompanionSources interface {
	CopyFile(context.Context, Execution, ExecutionFile) (VerifiedBlob, error)
}
