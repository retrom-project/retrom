package libraryimport

import (
	"context"
	"fmt"

	libraryservice "retrom/internal/service/libraryimport"
)

// Review draft commands are implemented by the application service and its
// persistence adapter. These aliases preserve the legacy libraryimport API
// while callers migrate to internal/service/libraryimport.
type (
	MetadataPatch  = libraryservice.MetadataPatch
	SelectedAssets = libraryservice.SelectedAssets
	DraftPatch     = libraryservice.DraftPatch
	DraftResult    = libraryservice.DraftResult
)

func validField(value string, maximum int, multiline bool) bool {
	return libraryservice.ValidReviewField(value, maximum, multiline)
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) PatchDraft(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	patch DraftPatch,
) (DraftResult, error) {
	if service.reviewDrafts == nil {
		return DraftResult{}, libraryservice.ErrInvalid
	}
	result, err := service.reviewDrafts.Patch(ctx, itemID, expectedVersion, patch)
	if err != nil {
		return DraftResult{}, fmt.Errorf("patch review draft: %w", err)
	}
	return result, nil
}
