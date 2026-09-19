package pegasusimport

import (
	"context"

	"retrom/internal/model/tagging"
)

type MappingReader interface {
	Import(context.Context, string) (Summary, error)
	CollectionOwner(context.Context, string) (string, error)
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
	LoadCollectionOwner(context.Context, string) (string, error)
	CommitMappingBatch(context.Context, MappingBatch) (Summary, error)
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
