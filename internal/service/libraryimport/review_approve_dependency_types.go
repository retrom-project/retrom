package libraryimport

import (
	"context"

	"retrom/internal/content/arcade"

	contentcapability "retrom/internal/content/capability"
	validation "retrom/internal/service/corevalidation"
)

type ApprovalDependencyInput struct {
	SnapshotID, ItemID, PlatformID, ProviderID, TargetID string
	ContentKind, DependencyJSON                          string
	Policy                                               contentcapability.Policy
}

type ApprovalDependencyScope struct {
	Reader ApprovalDependencyReader
	BIOS   validation.Repository
	Arcade arcade.RelationReader
}

type ApprovalDisc struct {
	Ordinal                             int
	State, LogicalName                  string
	FileRecord                          *string
	SourceFileRecord, SourceLogicalName *string
	SourceOrdinal, SizeBytes            *int64
}

type ApprovalMultiDisc struct {
	Discs                                                 []ApprovalDisc
	PlaylistCount, DiscCount, SourceCount, CanonicalCount int64
}

type ApprovalDependencyReader interface {
	LogicalName(context.Context, string) (string, error)
	MultiDisc(context.Context, string, string) (ApprovalMultiDisc, error)
	ArcadeRequirements(context.Context, string, string) (arcade.CatalogRequirements, error)
	ExternalFileCount(context.Context, string, string, string) (int64, error)
}
