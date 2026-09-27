package libraryimport

import (
	"context"

	"retrom/internal/service/importprogress"
)

// Publication is the durable decision of one import item. It freezes the
// validated business facts once, then permits directory publication to resume.
type Publication struct {
	IdentityDigest        string
	Files                 []PublicationFile
	Request               ReviewApprovalRequest
	ActorID, ProfileID    string
	Head                  ReviewApprovalHead
	Metadata              ApprovalMetadata
	Origin                ApprovalOrigin
	Assets                []ApprovalAsset
	ScreenshotIDs         []string
	GameID, VariantID     string
	ScreenshotOverride    bool
	RuntimeDependencyJSON string
	RPGProfile            RPGReviewProfile
	RPGDependencies       RPGReviewDependencies
}

type PublicationState struct {
	Found         bool
	State, GameID string
	Intent        *Publication
}

type PublicationRecords interface {
	PublicationFiles(context.Context, ReviewApprovalHead) ([]string, error)
	PreparePublicationRecords(context.Context, Publication) error
	ReadPublication(context.Context, string) (PublicationState, error)
	BeginPublication(context.Context, Publication, int64) error
	PublicationProgress(context.Context, string) (importprogress.Snapshot, int64, error)
}
