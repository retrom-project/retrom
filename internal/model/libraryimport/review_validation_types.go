package libraryimport

import (
	"context"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/corevalidation"
	corevalidationmodel "retrom/internal/model/corevalidation"
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
	PlatformID, CoreID, ProviderID, RuntimeTargetID                    string
	PlatformVersion                                                    int64
	DATVersionID                                                       *string
	ContentPolicy                                                      contentcapability.Policy
	DependencyFactsDigest                                              string
}

// ReviewValidationGuard identifies every mutable external fact used to build
// a validation plan. Draft version guards protect only the draft row itself;
// this value is carried alongside the plan so the repository can compare the
// planning inputs again in its write transaction.
type ReviewValidationGuard struct {
	SourceSnapshotID, SourceManifestDigest, ContentKind string
	TargetPlatformInstanceID                            string
	PlatformInstanceVersion                             int64
	PlatformID, CoreID, ProviderID, TargetID            string
	DATVersionID                                        *string
	DefaultDOSEntry                                     *string
	ContentPolicyDigest, DependencyFactsDigest          string
}

func (guard ReviewValidationGuard) Valid() bool {
	return guard.SourceSnapshotID != "" && guard.SourceManifestDigest != "" && guard.ContentKind != "" &&
		guard.TargetPlatformInstanceID != "" && guard.PlatformInstanceVersion >= 1 && guard.PlatformID != "" &&
		guard.CoreID != "" && guard.ProviderID != "" && guard.TargetID != "" && guard.ContentPolicyDigest != "" &&
		guard.DependencyFactsDigest != ""
}

// ReviewValidationRefreshRecord is the latest validation candidate for a
// draft. It is deliberately a value object shared by the refresh workflow and
// its storage adapter.
type ReviewValidationRefreshRecord struct {
	ID, SourceManifestDigest, PrepublishInputDigest                string
	Status, CompatibilityCode, DependencySnapshot                  string
	SourceSnapshotID, TargetPlatformInstanceID, CoreID, ProviderID string
	TargetID                                                       string
	DATVersionID, DefaultDOSEntry                                  *string
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
	Selected(context.Context, string, string) (ReviewValidationRefreshRecord, bool, error)
	Exact(context.Context, ReviewValidationRefreshLookup) (ReviewValidationRefreshRecord, bool, error)
	Fallback(context.Context, ReviewValidationRefreshLookup) (ReviewValidationRefreshRecord, bool, error)
	Create(context.Context, ReviewValidationRefreshCreate) error
	CopyFiles(context.Context, ReviewValidationRefreshFileCopy) error
	ContentLogicalName(context.Context, string) (string, error)
	RPGProfile(context.Context, string) (RPGReviewProfile, error)
	UpdateRPGDependencyDigest(context.Context, string, string, int64) error
}

// ReviewValidationRefreshReader is the read-only fact port used by the draft
// application service while it builds a validation write plan. It contains no
// transaction callback and no write operation; the repository applies the
// resulting plan later with the draft update.
type ReviewValidationRefreshReader interface {
	Inputs(context.Context, string, string) (ReviewValidationRefreshInputs, error)
	Exact(context.Context, ReviewValidationRefreshLookup) (ReviewValidationRefreshRecord, bool, error)
	Fallback(context.Context, ReviewValidationRefreshLookup) (ReviewValidationRefreshRecord, bool, error)
	ContentLogicalName(context.Context, string) (string, error)
	RPGProfile(context.Context, string) (RPGReviewProfile, error)
}

type ReviewValidationRefreshSelectedReader interface {
	Selected(context.Context, string, string) (ReviewValidationRefreshRecord, bool, error)
}

// ReviewValidationRefreshDependencyReader supplies the runtime dependency
// facts used by the validation planner. It is separate from the general
// refresh reader so each application port stays narrow and cohesive.
type ReviewValidationRefreshDependencyReader interface {
	BIOS(context.Context, string, string) ([]corevalidationmodel.BIOSRecord, error)
	ArcadeBIOS(context.Context, string, string, string) (corevalidation.BIOSDependency, bool, error)
}
