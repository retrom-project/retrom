package libraryimport

import (
	"context"

	"retrom/internal/contentcapability"
)

type ReviewValidationEvidence struct {
	DraftID                                                            string
	PlatformVersion                                                    int64
	SourceSnapshotID, PlatformInstanceID, CoreID, ProviderID, TargetID string
	ManifestDigest, InputDigest, Status, CompatibilityCode             string
	DependencyJSON                                                     string
	ValidationDAT, ValidationDOS, DraftDOS, ActiveDAT                  *string
	DraftSnapshotID, DraftPlatformInstanceID, SnapshotManifestDigest   string
	ContentKind                                                        string
	ContentPolicy                                                      contentcapability.Policy
	CurrentCoreID                                                      string
	CurrentPlatformVersion                                             int64
}

type ReviewValidationReader interface {
	Evidence(context.Context, string) (ReviewValidationEvidence, error)
	Profile(context.Context, string) (RPGReviewProfile, bool, error)
}
