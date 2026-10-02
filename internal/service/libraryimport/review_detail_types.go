package libraryimport

import (
	"context"
	"encoding/json"
	"errors"

	contentcapability "retrom/internal/content/capability"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

var ErrReviewNotFound = errors.New("REVIEW_NOT_FOUND")

type ReviewHead struct {
	ItemID, ImportJobID, DraftID, SnapshotID, ContentKind, PlatformID                             string
	PlatformInstance                                                                              ReviewPlatformInstance
	MetadataJSON, SourceManifestJSON                                                              string
	Version, UpdatedAtMS                                                                          int64
	Policy                                                                                        contentcapability.Policy
	ValidationStatus, CompatibilityCode, DependencyJSON                                           *string
	SelectedCandidateID, CoverID, UploadedCoverID, UploadedVideoID, BackgroundID, DefaultDOSEntry *string
}

type ReviewDetailRepository interface {
	WithRead(context.Context, func(ReviewReadScope) error) error
}
type ReviewReadScope struct {
	Drafts       ReviewDraftReader
	Media        ReviewMediaReader
	Sources      ReviewSourceReader
	Profiles     ReviewProfileReader
	Duplicates   ContentDuplicateReader
	Dependencies ReviewDependencyReader
	Metadata     metadatascrape.ReviewEvidenceReader
	Tags         tagging.ReferenceReader
}
type ReviewDraftReader interface {
	Head(context.Context, string) (ReviewHead, error)
	ScreenshotIDs(context.Context, string) ([]string, error)
	DOSEntries(context.Context, string) ([]ReviewDOSEntry, error)
}
type ReviewDetail struct {
	ItemID                string                           `json:"itemId"`
	ImportJobID           string                           `json:"importJobId"`
	Version               int64                            `json:"version"`
	UpdatedAtMS           int64                            `json:"updatedAtMs"`
	SnapshotID            string                           `json:"effectiveSourceSnapshotId"`
	PlatformInstance      ReviewPlatformInstance           `json:"platformInstance"`
	Metadata              json.RawMessage                  `json:"metadata"`
	SourceManifest        json.RawMessage                  `json:"sourceManifest"`
	Readiness             *ReviewReadinessView             `json:"readiness"`
	Candidates            []metadatascrape.ReviewCandidate `json:"candidates"`
	ScrapeRuns            []metadatascrape.ReviewRun       `json:"scrapeRuns"`
	CanApprove            bool                             `json:"canApprove"`
	UploadedAssets        []ReviewUploadedAsset            `json:"uploadedAssets"`
	SourceFiles           []ReviewSourceFile               `json:"sourceFiles"`
	SourceMedia           *ReviewSourceMedia               `json:"sourceMedia"`
	RuntimeScreenshot     *ReviewRuntimeScreenshot         `json:"runtimeScreenshot"`
	RPGMaker              *ReviewRPGMaker                  `json:"rpgMaker"`
	DuplicateGames        []DuplicateGame                  `json:"duplicateGames"`
	ContentIdentityDigest string                           `json:"contentIdentityDigest"`
	ArcadeDependencies    *ReviewArcade                    `json:"arcadeDependencies"`
	MultiDisc             *ReviewMultiDisc                 `json:"multiDisc"`
	SelectedCandidateID   *string                          `json:"selectedCandidateId"`
	DefaultDOSEntry       *string                          `json:"defaultDosEntry"`
	SelectedAssets        ReviewSelectedAssets             `json:"selectedAssets"`
	DOSEntries            []ReviewDOSEntry                 `json:"dosEntries"`
	Tags                  []tagging.Reference              `json:"tags"`
}
type ReviewPlatformInstance struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ReviewReadinessView struct {
	Status             string          `json:"status"`
	CompatibilityCode  string          `json:"compatibilityCode"`
	DependencySnapshot json.RawMessage `json:"dependencySnapshot"`
}
type ReviewSelectedAssets struct {
	CoverID         *string  `json:"coverCandidateAssetId"`
	UploadedCoverID *string  `json:"coverUploadedAssetId"`
	UploadedVideoID *string  `json:"videoUploadedAssetId"`
	BackgroundID    *string  `json:"backgroundCandidateAssetId"`
	ScreenshotIDs   []string `json:"screenshotCandidateAssetIds"`
}
type ReviewDOSEntry struct {
	Path             string `json:"path"`
	OriginalPath     string `json:"originalPath"`
	Kind             string `json:"kind"`
	Rank             int64  `json:"rank"`
	Enabled          bool   `json:"enabled"`
	DirectLaunchSafe bool   `json:"directLaunchSafe"`
}
