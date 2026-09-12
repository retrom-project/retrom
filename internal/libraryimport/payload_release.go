package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/payloadrelease"
	librarypersistence "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

func scheduleTerminalPayloads(
	ctx context.Context, transaction *sql.Tx, itemID, importID string, reason payloadrelease.Reason, now int64,
) error {
	outcome := libraryservice.ReviewOwnerDiscarded
	if reason != payloadrelease.ReasonImportDiscarded && reason != payloadrelease.ReasonImportPublished {
		return ErrInvalid
	}
	if reason == payloadrelease.ReasonImportPublished {
		outcome = libraryservice.ReviewOwnerPublished
	}

	err := librarypersistence.ScheduleReviewPayloads(ctx, transaction, libraryservice.ReviewPayloadRelease{
		ItemID: itemID, ImportID: importID, Outcome: outcome, NowMS: now,
	})
	if err != nil {
		return fmt.Errorf("libraryimport/schedule review payload: %w", err)
	}
	return nil
}
