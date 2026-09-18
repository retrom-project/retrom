package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

type Screenshots struct {
	database      *sql.DB
	preCommitHook func() error
}

func NewScreenshots(database *sql.DB) *Screenshots { return &Screenshots{database: database} }

func (repository *Screenshots) WithPreCommitHook(hook func() error) {
	repository.preCommitHook = hook
}

type screenshotRecords struct{ executor dbexec.Executor }

const screenshotColumns = `preview.id,preview.import_item_id,preview.source_snapshot_id,
preview.target_platform_instance_id,preview.validation_id,preview.provider_id,preview.target_id,
preview.credential_sha256,preview.state,preview.hard_expires_at_ms`

func (repository *Screenshots) Preview(ctx context.Context, id string) (application.ScreenshotSource, bool, error) {
	return readScreenshotSource(repository.database.QueryRowContext(ctx, `SELECT `+screenshotColumns+`
FROM review_preview_sessions preview WHERE preview.id=?`, id))
}

func (repository *Screenshots) LoadScreenshotSource(
	ctx context.Context, id string,
) (application.ScreenshotSource, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ScreenshotSource{}, false,
			fmt.Errorf("begin screenshot source read: %w", err)
	}
	defer dbexec.Rollback(tx)
	source, found, err := (screenshotRecords{executor: tx}).Current(ctx, id)
	if err != nil {
		return application.ScreenshotSource{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.ScreenshotSource{}, false,
			fmt.Errorf("commit screenshot source read: %w", err)
	}
	return source, found, nil
}

func (repository *Screenshots) CommitScreenshot(ctx context.Context, plan application.ScreenshotWrite) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review screenshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := (screenshotRecords{executor: tx}).Replace(ctx, plan); err != nil {
		return err
	}
	if repository.preCommitHook != nil {
		if err := repository.preCommitHook(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit review screenshot: %w", err)
	}
	return nil
}

func (records screenshotRecords) Current(ctx context.Context, id string) (application.ScreenshotSource, bool, error) {
	return readScreenshotSource(records.executor.QueryRowContext(ctx, `SELECT `+screenshotColumns+`
FROM review_preview_sessions preview
JOIN import_items item ON item.id=preview.import_item_id
 AND item.state='REVIEW_PENDING' AND item.payload_state='RETAINED'
JOIN review_drafts draft ON draft.import_item_id=item.id
 AND draft.effective_source_snapshot_id=preview.source_snapshot_id
 AND draft.target_platform_instance_id=preview.target_platform_instance_id
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
 AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
JOIN import_item_core_validations validation ON validation.id=preview.validation_id
 AND validation.import_item_id=preview.import_item_id
 AND validation.source_snapshot_id=preview.source_snapshot_id
 AND validation.target_platform_instance_id=preview.target_platform_instance_id
 AND validation.provider_id=preview.provider_id AND validation.target_id=preview.target_id
 AND validation.id=(
  SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=preview.import_item_id
   AND candidate.source_snapshot_id=preview.source_snapshot_id
   AND candidate.target_platform_instance_id=preview.target_platform_instance_id
   AND candidate.provider_id=preview.provider_id AND candidate.target_id=preview.target_id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
 )
WHERE preview.id=?`, id))
}

func readScreenshotSource(row dbexec.Scanner) (application.ScreenshotSource, bool, error) {
	var source application.ScreenshotSource
	err := row.Scan(
		&source.PreviewID,
		&source.ItemID,
		&source.SourceSnapshotID,
		&source.PlatformInstanceID,
		&source.ValidationID,
		&source.ProviderID,
		&source.TargetID,
		&source.CredentialHash,
		&source.State,
		&source.HardExpiresAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ScreenshotSource{}, false, nil
	}
	if err != nil {
		return application.ScreenshotSource{}, false, fmt.Errorf("query screenshot source: %w", err)
	}
	return source, true, nil
}
