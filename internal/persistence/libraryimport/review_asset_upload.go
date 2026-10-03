package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

type (
	ReviewAssetUploads struct{ database dbapi.DB }
	reviewAssetRecords struct{ executor dbapi.Executor }
)

func NewReviewAssetUploads(database dbapi.DB) *ReviewAssetUploads {
	return &ReviewAssetUploads{database: database}
}

func (repository *ReviewAssetUploads) Source(
	ctx context.Context, fileID string,
) (libraryservice.ReviewAssetSource, bool, error) {
	return (reviewAssetRecords{repository.database}).Source(ctx, fileID)
}

func (repository *ReviewAssetUploads) WithWrite(
	ctx context.Context, work func(libraryservice.ReviewAssetScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin review asset transaction: %w", err)
	}
	defer dbapi.Rollback(transaction)
	records := reviewAssetRecords{transaction}
	if err := work(libraryservice.ReviewAssetScope{Reader: records, Writer: records}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit review asset transaction: %w", err)
	}
	return nil
}

func (records reviewAssetRecords) Source(
	ctx context.Context, fileID string,
) (libraryservice.ReviewAssetSource, bool, error) {
	var source libraryservice.ReviewAssetSource
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT f.id,f.upload_session_id,b.value,json_extract(b.value, '$.sha256'),upload.purpose,
json_extract(b.value, '$.size_bytes')
FROM import_files f
JOIN json_each(json_array(f.file_record)) b ON b.value IS NOT NULL
JOIN upload_sessions upload ON upload.id=f.upload_session_id
WHERE f.id=? AND f.released_at_ms IS NULL
`, fileID).Scan(
		&source.FileID,
		&source.UploadID,
		&source.FileRecord,
		&source.Digest,
		&source.Purpose,
		&source.SizeBytes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ReviewAssetSource{}, false, nil
	}
	if err != nil {
		return libraryservice.ReviewAssetSource{}, false, fmt.Errorf("query review asset source: %w", err)
	}
	return source, true, nil
}

func (records reviewAssetRecords) Draft(
	ctx context.Context, itemID string,
) (libraryservice.ReviewAssetDraft, bool, error) {
	var draft libraryservice.ReviewAssetDraft
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT d.review_version,i.state,
(EXISTS(SELECT 1 FROM source_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state<>'REVIEW_PENDING'))
FROM import_items d JOIN import_items i ON i.id=d.id WHERE i.id=?
`, itemID).Scan(&draft.Version, &draft.State, &draft.SourceBusy)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ReviewAssetDraft{}, false, nil
	}
	if err != nil {
		return libraryservice.ReviewAssetDraft{}, false, fmt.Errorf("query review asset draft: %w", err)
	}
	return draft, true, nil
}

func (records reviewAssetRecords) ExistingByUpload(
	ctx context.Context, fileID string,
) (libraryservice.ReviewAssetExisting, bool, error) {
	var existing libraryservice.ReviewAssetExisting
	asset := &existing.Record
	err := dbapi.QueryRowContext(
		ctx,
		records.executor,
		`
SELECT a.id,a.import_item_id,a.upload_file_id,a.file_record,a.kind,a.media_type,a.width_px,a.height_px,
a.created_at_ms,
EXISTS(SELECT 1 FROM upload_consumptions c JOIN import_files f ON f.id=a.upload_file_id
 WHERE c.consumer_type='REVIEW_ASSET' AND c.consumer_id=a.id AND c.upload_file_id=a.upload_file_id
 AND c.upload_session_id=f.upload_session_id AND c.released_at_ms IS NULL)
FROM review_uploaded_assets a WHERE a.upload_file_id=?
`,
		fileID,
	).Scan(
		&asset.ID,
		&asset.ItemID,
		&asset.UploadFileID,
		&asset.FileRecord,
		&asset.Kind,
		&asset.MediaType,
		&asset.Width,
		&asset.Height,
		&asset.CreatedAtMS,
		&existing.HasConsumption,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ReviewAssetExisting{}, false, nil
	}
	if err != nil {
		return libraryservice.ReviewAssetExisting{}, false, fmt.Errorf("query review asset owner: %w", err)
	}
	return existing, true, nil
}

func (records reviewAssetRecords) InsertAsset(
	ctx context.Context,
	asset libraryservice.ReviewAssetRecord,
) error {
	_, err := recordstore.InsertRows(
		ctx,
		records.executor,
		"review_uploaded_assets",
		`
INSERT INTO review_uploaded_assets(
id,import_item_id,upload_file_id,file_record,kind,width_px,height_px,media_type,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`,
		asset.ID,
		asset.ItemID,
		asset.UploadFileID,
		asset.FileRecord,
		asset.Kind,
		asset.Width,
		asset.Height,
		asset.MediaType,
		asset.CreatedAtMS,
	)
	if err != nil {
		return fmt.Errorf("insert review uploaded asset: %w", err)
	}
	return nil
}

func (records reviewAssetRecords) Consume(
	ctx context.Context,
	consumption libraryservice.ReviewAssetConsumption,
) error {
	_, err := recordstore.CreateUploadConsumptions(
		ctx,
		records.executor,
		`
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,
created_at_ms)
VALUES(?,?,?,'REVIEW_ASSET',?,?)
`,
		consumption.ID,
		consumption.UploadID,
		consumption.FileID,
		consumption.AssetID,
		consumption.CreatedAtMS,
	)
	if err != nil {
		return fmt.Errorf("insert review asset consumption: %w", err)
	}
	return nil
}
