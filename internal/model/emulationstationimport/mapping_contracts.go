package emulationstationimport

import (
	"context"

	"retrom/internal/model/tagging"
)

type MappingReader interface {
	Import(context.Context, string) (Summary, error)
	Collection(context.Context, string) (MappingCollection, error)
	EligibleTarget(context.Context, string) (MappingTarget, bool, error)
}

type MappingBatchEntry struct {
	Change  CollectionMapping
	Owner   tagging.Owner
	TagIDs  []string
	ActorID string
}

type MappingBatch struct {
	ImportID string
	Entries  []MappingBatchEntry
	Advance  MappingAdvance
}

type MappingRepository interface {
	LoadImportSummary(context.Context, string) (Summary, error)
	LoadMappingCollection(context.Context, string) (MappingCollection, error)
	LoadEligibleTarget(context.Context, string) (MappingTarget, bool, error)
	CommitMappingBatch(context.Context, MappingBatch) (Summary, error)
}

type MappingCollection struct {
	ImportID  string
	GameCount int64
}

type MappingTarget struct {
	InstanceID                               string
	InstanceVersion                          int64
	PlatformID, CoreID, ProviderID, TargetID string
	DATVersionID                             *string
}

type CollectionMapping struct {
	ImportID string
	Mapping  Mapping
	Target   *MappingTarget
	Tags     []tagging.Reference
	NowMS    int64
}

type MappingAdvance struct {
	Before Summary
	NowMS  int64
}
