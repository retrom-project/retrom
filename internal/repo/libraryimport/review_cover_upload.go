package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
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

func (repository *ReviewCoverUploads) CommitCoverUpload(
	ctx context.Context, cmd application.ReviewCoverUploadCommand,
) (application.ReviewCoverRecord, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return application.ReviewCoverRecord{}, fmt.Errorf("begin review cover transaction: %w", err)
	}
	defer dbexec.Rollback(transaction)
	records := reviewCoverRecords{transaction}

	if err := checkCoverAuthority(ctx, records, cmd.Request, cmd.Source); err != nil {
		return application.ReviewCoverRecord{}, err
	}
	existing, found, err := records.ExistingByUpload(ctx, cmd.Source.FileID)
	if err != nil {
		return application.ReviewCoverRecord{}, fmt.Errorf("read review cover ownership: %w", err)
	}
	if found {
		if existing.Record.ItemID != cmd.Request.ItemID {
			return application.ReviewCoverRecord{}, application.ErrReviewCoverConsumed
		}
		if !existing.HasConsumption || existing.Record.BlobID != cmd.Source.BlobID {
			return application.ReviewCoverRecord{}, application.ErrReviewCoverIntegrity
		}
		if err := transaction.Commit(); err != nil {
			return application.ReviewCoverRecord{}, fmt.Errorf("commit review cover transaction: %w", err)
		}
		return existing.Record, nil
	}
	record := application.ReviewCoverRecord{
		ID: cmd.AssetID, ItemID: cmd.Request.ItemID, UploadFileID: cmd.Source.FileID,
		BlobID: cmd.Source.BlobID, Width: cmd.Width, Height: cmd.Height,
		MediaType: cmd.MediaType, CreatedAtMS: cmd.NowMS,
	}
	if err := records.InsertAsset(ctx, record); err != nil {
		return application.ReviewCoverRecord{}, fmt.Errorf("save review cover asset: %w", err)
	}
	if err := records.Consume(ctx, application.ReviewCoverConsumption{
		ID: cmd.ConsumptionID, UploadID: cmd.Source.UploadID, FileID: cmd.Source.FileID,
		AssetID: cmd.AssetID, CreatedAtMS: cmd.NowMS,
	}); err != nil {
		return application.ReviewCoverRecord{}, fmt.Errorf("retain review cover upload: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return application.ReviewCoverRecord{}, fmt.Errorf("commit review cover transaction: %w", err)
	}
	return record, nil
}

func checkCoverAuthority(
	ctx context.Context,
	records reviewCoverRecords,
	request application.ReviewCoverRequest,
	prepared application.ReviewCoverSource,
) error {
	current, found, err := records.Source(ctx, request.UploadFileID)
	if err != nil {
		return fmt.Errorf("recheck review cover source: %w", err)
	}
	if !found || current != prepared {
		return application.ErrReviewCoverUploadInvalid
	}
	draft, found, err := records.Draft(ctx, request.ItemID)
	if err != nil {
		return fmt.Errorf("read review cover authority: %w", err)
	}
	if !found || draft.Version != request.ExpectedVersion || draft.State != "REVIEW_PENDING" ||
		(draft.HandoffKind != "DIRECT" && !draft.EmulationStationReady) || draft.SourceBusy {
		return application.ErrReviewCoverVersion
	}
	return nil
}

func (records reviewCoverRecords) Source(
	ctx context.Context, fileID string,
) (application.ReviewCoverSource, bool, error) {
	var source application.ReviewCoverSource
	err := records.executor.QueryRowContext(ctx, `
SELECT f.id,f.upload_session_id,b.id,b.sha256,upload.purpose,b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
JOIN upload_sessions upload ON upload.id=f.upload_session_id
WHERE f.id=? AND f.state='COMPLETE'
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
SELECT d.version,i.state,i.review_handoff_kind,
EXISTS(SELECT 1 FROM emulationstation_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state='REVIEW_PENDING'),
(EXISTS(SELECT 1 FROM pegasus_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state<>'REVIEW_PENDING') OR
 EXISTS(SELECT 1 FROM emulationstation_import_items source
 WHERE source.library_import_item_id=i.id AND source.execution_state<>'REVIEW_PENDING'))
FROM review_drafts d JOIN import_items i ON i.id=d.import_item_id WHERE i.id=?
`, itemID).Scan(&draft.Version, &draft.State, &draft.HandoffKind, &draft.EmulationStationReady, &draft.SourceBusy)
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
EXISTS(SELECT 1 FROM upload_consumptions c JOIN upload_files f ON f.id=a.upload_file_id
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
