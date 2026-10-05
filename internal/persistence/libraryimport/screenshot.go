package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/contentquery"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type Screenshots struct{ database dbapi.DB }

func NewScreenshots(database dbapi.DB) *Screenshots { return &Screenshots{database: database} }

type screenshotRecords struct{ executor dbapi.Executor }

const screenshotColumns = `preview.id,review_binding.import_item_id,review_binding.source_snapshot_id,
preview.target_platform_instance_id,preview.provider_id,preview.target_id,
preview.credential_sha256,preview.state,preview.hard_expires_at_ms`

func (repository *Screenshots) Preview(ctx context.Context, id string) (libraryservice.ScreenshotSource, bool, error) {
	return readScreenshotSource(dbapi.QueryRowContext(ctx, repository.database, `SELECT `+screenshotColumns+`
FROM runtime_preview_sessions preview
JOIN review_preview_bindings review_binding ON review_binding.preview_session_id=preview.id WHERE preview.id=?`, id))
}

func (repository *Screenshots) WithScreenshot(
	ctx context.Context, work func(libraryservice.ScreenshotScope) error,
) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		if err := work(screenshotRecords{executor: tx}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit review screenshot: %w", err)
	}
	return nil
}

func (records screenshotRecords) Current(
	ctx context.Context, id string,
) (libraryservice.ScreenshotSource, bool, error) {
	return readScreenshotSource(dbapi.QueryRowContext(ctx, records.executor, `SELECT `+screenshotColumns+`
FROM runtime_preview_sessions preview
JOIN review_preview_bindings review_binding ON review_binding.preview_session_id=preview.id
JOIN import_items item ON item.id=review_binding.import_item_id
 AND item.state='REVIEW_PENDING' AND item.payload_state='RETAINED'
JOIN import_items draft ON draft.id=item.id
 AND draft.effective_source_snapshot_id=review_binding.source_snapshot_id
 AND draft.target_platform_instance_id=preview.target_platform_instance_id
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
 AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
JOIN (`+contentquery.CurrentContentSQL+`) content ON content.import_item_id=review_binding.import_item_id
 AND content.provider_id=preview.provider_id AND content.target_id=preview.target_id
WHERE preview.id=?`, id))
}

func readScreenshotSource(row dbapi.Scanner) (libraryservice.ScreenshotSource, bool, error) {
	var source libraryservice.ScreenshotSource
	err := row.Scan(
		&source.PreviewID,
		&source.ItemID,
		&source.SourceSnapshotID,
		&source.PlatformInstanceID,
		&source.ProviderID,
		&source.TargetID,
		&source.CredentialHash,
		&source.State,
		&source.HardExpiresAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ScreenshotSource{}, false, nil
	}
	if err != nil {
		return libraryservice.ScreenshotSource{}, false, fmt.Errorf("query screenshot source: %w", err)
	}
	return source, true, nil
}
