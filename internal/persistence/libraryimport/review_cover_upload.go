package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

type (
	ReviewCoverUploads struct{ database *sql.DB }
	reviewCoverRecords struct{ executor dbexec.Executor }
)

func NewReviewCoverUploads(database *sql.DB) *ReviewCoverUploads {
	return &ReviewCoverUploads{database: database}
}

func (repository *ReviewCoverUploads) Source(
	ctx context.Context, fileID string,
) (application.ReviewCoverSource, bool, error) {
	return (reviewCoverRecords{repository.database}).Source(ctx, fileID)
}

func (repository *ReviewCoverUploads) WithWrite(
	ctx context.Context, work func(application.ReviewCoverScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review cover transaction: %w", err)
	}
	defer dbexec.Rollback(transaction)
	records := reviewCoverRecords{transaction}
	if err := work(application.ReviewCoverScope{Reader: records, Writer: records}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit review cover transaction: %w", err)
	}
	return nil
}

func (records reviewCoverRecords) Source(
	ctx context.Context, fileID string,
) (application.ReviewCoverSource, bool, error) {
	var source application.ReviewCoverSource
	err := records.executor.QueryRowContext(ctx, `
SELECT f.id,f.upload_session_id,b.id,b.sha256,upload.purpose,b.size_bytes
FROM import_files f
JOIN blobs b ON b.id=f.blob_id
JOIN upload_sessions upload ON upload.id=f.upload_session_id
WHERE f.id=? AND f.released_at_ms IS NULL
`, fileID).Scan(&source.FileID, &source.UploadID, &source.BlobID, &source.Digest, &source.Purpose, &source.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewCoverSource{}, false, nil
	}
	if err != nil {
		return application.ReviewCoverSource{}, false, fmt.Errorf("query review cover source: %w", err)
	}
	return source, true, nil
}

func (records reviewCoverRecords) Draft(
	ctx context.Context, itemID string,
) (application.ReviewCoverDraft, bool, error) {
	var draft application.ReviewCoverDraft
	err := records.executor.QueryRowContext(ctx, `
SELECT d.version,i.state,
EXISTS(SELECT 1 FROM source_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state='REVIEW_PENDING'),
(EXISTS(SELECT 1 FROM source_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state<>'REVIEW_PENDING'))
FROM review_drafts d JOIN import_items i ON i.id=d.import_item_id WHERE i.id=?
`, itemID).Scan(&draft.Version, &draft.State, &draft.SourceReady, &draft.SourceBusy)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewCoverDraft{}, false, nil
	}
	if err != nil {
		return application.ReviewCoverDraft{}, false, fmt.Errorf("query review cover draft: %w", err)
	}
	return draft, true, nil
}

func (records reviewCoverRecords) ExistingByUpload(
	ctx context.Context, fileID string,
) (application.ReviewCoverExisting, bool, error) {
	var existing application.ReviewCoverExisting
	asset := &existing.Record
	err := records.executor.QueryRowContext(ctx, `
SELECT a.id,a.import_item_id,a.upload_file_id,a.blob_id,a.media_type,a.width_px,a.height_px,a.created_at_ms,
EXISTS(SELECT 1 FROM upload_consumptions c JOIN import_files f ON f.id=a.upload_file_id
 WHERE c.consumer_type='REVIEW_ASSET' AND c.consumer_id=a.id AND c.upload_file_id=a.upload_file_id
 AND c.upload_session_id=f.upload_session_id AND c.released_at_ms IS NULL)
FROM review_uploaded_assets a WHERE a.upload_file_id=?
`, fileID).Scan(&asset.ID, &asset.ItemID, &asset.UploadFileID, &asset.BlobID, &asset.MediaType,
		&asset.Width, &asset.Height, &asset.CreatedAtMS, &existing.HasConsumption)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewCoverExisting{}, false, nil
	}
	if err != nil {
		return application.ReviewCoverExisting{}, false, fmt.Errorf("query review cover owner: %w", err)
	}
	return existing, true, nil
}

func (records reviewCoverRecords) InsertAsset(ctx context.Context, asset application.ReviewCoverRecord) error {
	_, err := records.executor.ExecContext(ctx, `
INSERT INTO review_uploaded_assets(
id,import_item_id,upload_file_id,blob_id,kind,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?,?,'COVER',?,?,?,?)
`, asset.ID, asset.ItemID, asset.UploadFileID, asset.BlobID,
		asset.Width, asset.Height, asset.MediaType, asset.CreatedAtMS)
	if err != nil {
		return fmt.Errorf("insert review uploaded asset: %w", err)
	}
	return nil
}

func (records reviewCoverRecords) Consume(ctx context.Context, consumption application.ReviewCoverConsumption) error {
	_, err := recordstore.CreateUploadConsumptions(ctx, records.executor, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,?,'REVIEW_ASSET',?,?)
`, consumption.ID, consumption.UploadID, consumption.FileID, consumption.AssetID, consumption.CreatedAtMS)
	if err != nil {
		return fmt.Errorf("insert review cover consumption: %w", err)
	}
	return nil
}
