package pegasusimport

import (
	"context"

	"retrom/internal/model/tagging"
)

type MappingScope struct {
	Read  MappingReader
	Write MappingWriter
	Tags  tagging.WriteScope
}

type MappingReader interface {
	Import(context.Context, string) (Summary, error)
	CollectionOwner(context.Context, string) (string, error)
	EligibleTarget(context.Context, string) (MappingTarget, bool, error)
}

type MappingWriter interface {
	Put(context.Context, CollectionMapping) error
	Advance(context.Context, MappingAdvance) error
}

type MappingRepository interface {
	WithMappings(context.Context, func(MappingScope) error) error
}

type MappingTagWriter interface {
	ReplacePegasusCollectionTags(
		context.Context, tagging.WriteScope, string, []string, string, int64,
	) ([]tagging.Reference, error)
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
