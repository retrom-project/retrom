package libraryimport

import (
	"context"

	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/tagging"
)

// Cross-domain ports expose only the operations used by these workflows.
// Write scopes are supplied by the active import or approval transaction.
type ImportTagReferences interface {
	ValidateReferences(context.Context, tagging.WriteScope, []string) ([]tagging.Reference, error)
}
type ImportCreationTags interface {
	ImportTagReferences
	AssignReviewDraftTags(context.Context, tagging.WriteScope, string, []tagging.Reference, string, int64) error
}
type ReviewApprovalTags interface {
	CopyDraftTagsToGame(context.Context, tagging.WriteScope, string, string, string, int64) ([]tagging.Reference, error)
}
type ImportMetadata interface {
	ScheduleImport(context.Context, metadatascrape.ScheduleScope, string, string) (metadatascrape.Scheduled, error)
	Dispatch(context.Context, string) bool
}
