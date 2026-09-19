package libraryimport

import (
	"context"

	"retrom/internal/capability/content/contentcapability"
)

type ApprovalDependencyInput struct {
	SnapshotID, ValidationID, PlatformID, ProviderID, TargetID string
	ContentKind, DependencyJSON                                string
	Policy                                                     contentcapability.Policy
}

type ApprovalDependencyScope struct {
	Reader ApprovalDependencyReader
	BIOS   BIOSFactsReader
	Arcade ArcadeRelationReader
}

type ApprovalDisc struct {
	Ordinal                         int
	State, LogicalName              string
	BlobID                          *string
	SourceBlobID, SourceLogicalName *string
	SourceOrdinal, SizeBytes        *int64
}

type ApprovalMultiDisc struct {
	Discs                                                 []ApprovalDisc
	PlaylistCount, DiscCount, SourceCount, CanonicalCount int64
}

type (
	ApprovalArcadeROM          = ArcadeROMRequirement
	ApprovalArcadeRequirements = ArcadeCatalogRequirements
)

type ApprovalDependencyReader interface {
	LogicalName(context.Context, string) (string, error)
	MultiDisc(context.Context, string, string) (ApprovalMultiDisc, error)
	ArcadeRequirements(context.Context, string, string) (ApprovalArcadeRequirements, error)
	ExternalFileCount(context.Context, string, string, string) (int64, error)
}
