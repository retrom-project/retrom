package libraryimport

import (
	"context"

	"retrom/internal/contentcapability"
	"retrom/internal/corevalidation"
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

// ReviewValidationRefreshInputs are the immutable facts selected for a draft
// validation refresh. Nullable database values are represented as pointers so
// the application port does not expose driver-specific details.
type ReviewValidationRefreshInputs struct {
	DraftID, EffectiveSnapshotID, EffectiveManifestDigest, ContentKind string
	PlatformVersion                                                    int64
	CoreID, ProviderID, RuntimeTargetID                                string
	DATVersionID                                                       *string
	ContentPolicy                                                      contentcapability.Policy
}

// ReviewValidationRefreshRecord is the latest validation candidate for a
// draft. It is deliberately a value object shared by the refresh workflow and
// its storage adapter.
type ReviewValidationRefreshRecord struct {
	ID, SourceManifestDigest, PrepublishInputDigest string
	Status, CompatibilityCode, DependencySnapshot   string
}

type ReviewValidationRefreshLookup struct {
	ItemID, SourceSnapshotID, TargetPlatformInstanceID string
	CoreID, ProviderID, TargetID                       string
	DATVersionID, DefaultDOSEntry                      *string
}

type ReviewValidationRefreshCreate struct {
	ID, ItemID, TargetPlatformInstanceID, CoreID      string
	ProviderID, TargetID, SourceManifestDigest        string
	SourceSnapshotID, PrepublishInputDigest           string
	Status, CompatibilityCode, DependencySnapshotJSON string
	PlatformInstanceVersion                           int64
	DATVersionID, DefaultDOSEntry                     *string
	CreatedAtMS                                       int64
}

type ReviewValidationRefreshFileCopy struct {
	ValidationID, SourceValidationID string
	CreatedAtMS                      int64
	ReplaceBIOSBundle                bool
	Dependencies                     []corevalidation.BIOSDependency
}

// ReviewValidationRefreshRepository owns all relational reads and writes
// needed while a review draft chooses or rebuilds a validation. The caller
// supplies a transaction-bound implementation, preserving the draft patch's
// atomicity without leaking storage details into the business package.
//
//nolint:interfacebloat // one refresh transaction needs this cohesive read/write port
type ReviewValidationRefreshRepository interface {
	Inputs(context.Context, string, string) (ReviewValidationRefreshInputs, error)
	Exact(context.Context, ReviewValidationRefreshLookup) (ReviewValidationRefreshRecord, bool, error)
	Fallback(context.Context, ReviewValidationRefreshLookup) (ReviewValidationRefreshRecord, bool, error)
	Create(context.Context, ReviewValidationRefreshCreate) error
	CopyFiles(context.Context, ReviewValidationRefreshFileCopy) error
	ContentLogicalName(context.Context, string) (string, error)
	RPGProfile(context.Context, string) (RPGReviewProfile, error)
	UpdateRPGDependencyDigest(context.Context, string, string, int64) error
}
