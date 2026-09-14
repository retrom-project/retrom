package libraryimport

import "context"

type ReviewPreviewValidationRepository interface {
	Refresh(context.Context, string, int64) error
}
