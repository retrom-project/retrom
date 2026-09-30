package libraryimport

import (
	"context"

	launch "retrom/internal/service/launch"
)

// ReviewPreviewScope owns the current review input and session write in one transaction.
type ReviewPreviewScope interface {
	launch.PreviewSessionScope
	Replay(context.Context, string, string) (launch.PreviewReceipt, bool, error)
	Current(context.Context, launch.ReviewPreviewRequest) (launch.PreviewSnapshot, string, bool, error)
}
type ReviewPreviewRepository interface {
	Restore(context.Context, string) (launch.PreviewRestore, bool, error)
	Replay(context.Context, string, string) (launch.PreviewReceipt, bool, error)
	Snapshot(context.Context, string) (launch.PreviewSnapshot, bool, error)
	WithCreation(context.Context, func(ReviewPreviewScope) error) error
}
