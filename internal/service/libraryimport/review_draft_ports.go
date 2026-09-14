package libraryimport

import (
	"context"

	application "retrom/internal/model/libraryimport"
)

// ReviewDraftPatchRepository is the application-facing draft port. Reads and
// writes are separate so the service can make a value decision without giving
// persistence a callback into the application layer.
type ReviewDraftPatchRepository interface {
	LoadPatchSnapshot(context.Context, application.ReviewDraftPatchQuery) (application.ReviewDraftPatchSnapshot, error)
	CommitPatch(context.Context, application.ReviewDraftWritePlan) (application.DraftResult, error)
}

type ReviewDraftValidationPort interface {
	Resolve(context.Context, ReviewDraftValidationRequest) (application.ReviewValidationPlan, error)
	ResolveSelected(context.Context, ReviewDraftSelectedValidationRequest) (application.ReviewValidationPlan, error)
	SelectScummVM(context.Context, ReviewDraftScummVMRequest) (application.ReviewValidationPlan, error)
}

type ReviewDraftValidationReader = application.ReviewValidationRefreshReader

type ReviewDraftValidationSelectedReader = application.ReviewValidationRefreshSelectedReader

type ReviewDraftValidationDependencyReader = application.ReviewValidationRefreshDependencyReader

type ReviewDraftValidationRequest struct {
	ItemID, TargetPlatformInstanceID string
	DefaultDOSEntry                  *string
	RPGSelfContainedOverride         *bool
}

type ReviewDraftSelectedValidationRequest struct {
	ItemID, TargetPlatformInstanceID, ValidationID string
	DefaultDOSEntry                                *string
}

type ReviewDraftScummVMRequest struct {
	ItemID, TargetPlatformInstanceID string
	DefaultDOSEntry                  *string
	CandidateID                      string
}
