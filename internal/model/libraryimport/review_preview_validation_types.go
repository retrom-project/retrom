package libraryimport

import "context"

// ReviewPreviewValidationDraft is the storage-neutral state required by the
// preview refresh planner.
type ReviewPreviewValidationDraft struct {
	ItemID, TargetID, Selected string
	DOSEntry                   *string
	Version                    int64
}

// ReviewPreviewValidationPlan contains only values and optimistic guards. The
// repository applies validation creation, file copying, and selection in one
// transaction without calling back into the service layer.
type ReviewPreviewValidationPlan struct {
	ItemID, ValidationID                       string
	ExpectedVersion                            int64
	ExpectedSelectedValidation                 string
	ExpectedSelectedValidationPrepublishDigest string
	ExpectedValidationGuard                    ReviewValidationGuard
	Create                                     *ReviewValidationRefreshCreate
	Copy                                       *ReviewValidationRefreshFileCopy
	NowMS                                      int64
}

type ReviewPreviewValidationRepository interface {
	Draft(context.Context, string) (ReviewPreviewValidationDraft, error)
	Commit(context.Context, ReviewPreviewValidationPlan) error
}
