package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"

	application "retrom/internal/service/launch"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/blobcatalog"

	"retrom/internal/persistence/recordstore"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/mediaasset"
)

var (
	ErrReviewPreviewUnavailable = application.ErrReviewPreviewUnavailable
	ErrReviewScreenshotInvalid  = errors.New("REVIEW_SCREENSHOT_INVALID")
)

type (
	ReviewPreviewRequest = application.ReviewPreviewRequest
	ReviewPreviewCreated = application.ReviewPreviewCreated
)

func (service *Service) ReviewPreviewConfig(ctx context.Context, id, capability string) (Config, error) {
	configuration, err := service.configIssuer().Issue(ctx, application.SessionRef{ID: id, Preview: true}, capability)
	if err != nil {
		return Config{}, fmt.Errorf("review config: %w", err)
	}
	return configuration, nil
}

type ReviewScreenshot struct {
	ID, ImportItemID, ValidationID  string
	ProviderID, TargetID            string
	WidthPX, HeightPX, CapturedAtMS int64
}

type reviewScreenshotImage struct {
	Blob  blobstore.Metadata
	Image mediaasset.Image
}

type reviewScreenshotTarget struct {
	ItemID, SourceSnapshotID, ValidationID string
	ProviderID, TargetID                   string
}

func (service *Service) StoreReviewScreenshot(
	ctx context.Context,
	previewID, capability string,
	reader io.Reader,
) (ReviewScreenshot, error) {
	if service.blobs == nil {
		return ReviewScreenshot{}, ErrReviewScreenshotInvalid
	}
	if err := service.authorizeReviewScreenshot(ctx, previewID, capability); err != nil {
		return ReviewScreenshot{}, err
	}
	image, err := service.inspectReviewScreenshot(reader)
	if err != nil {
		return ReviewScreenshot{}, err
	}
	return service.persistReviewScreenshot(ctx, previewID, capability, image)
}

func (service *Service) inspectReviewScreenshot(reader io.Reader) (reviewScreenshotImage, error) {
	metadata, err := service.blobs.Put(reader)
	if err != nil {
		return reviewScreenshotImage{}, ErrReviewScreenshotInvalid
	}
	file, err := os.Open(metadata.Path)
	if err != nil {
		return reviewScreenshotImage{}, ErrReviewScreenshotInvalid
	}
	image, inspectErr := mediaasset.InspectImage(file, metadata.Size)
	cleanup.Error("close", file.Close())
	if inspectErr != nil || image.MediaType != "image/png" && image.MediaType != "image/jpeg" {
		return reviewScreenshotImage{}, ErrReviewScreenshotInvalid
	}
	return reviewScreenshotImage{Blob: metadata, Image: image}, nil
}

func (service *Service) persistReviewScreenshot(
	ctx context.Context,
	previewID, capability string,
	image reviewScreenshotImage,
) (ReviewScreenshot, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("review screenshot: %w", err)
	}
	defer dbexec.Rollback(transaction)
	target, err := service.reviewScreenshotTarget(ctx, transaction, previewID, capability)
	if err != nil {
		return ReviewScreenshot{}, err
	}
	now := service.now().UnixMilli()
	blobID, err := blobcatalog.EnsureRecord(ctx, transaction, image.Blob, image.Image.MediaType, now)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("register review screenshot: %w", err)
	}
	screenshotID, err := uuid.NewV7()
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("review screenshot: %w", err)
	}
	if err := insertReviewScreenshot(
		ctx, transaction, screenshotID.String(), previewID, target, blobID, image.Image, now,
	); err != nil {
		return ReviewScreenshot{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ReviewScreenshot{}, fmt.Errorf("commit review screenshot: %w", err)
	}
	return ReviewScreenshot{
		ID: screenshotID.String(), ImportItemID: target.ItemID, ValidationID: target.ValidationID,
		ProviderID: target.ProviderID, TargetID: target.TargetID,
		WidthPX: image.Image.WidthPX, HeightPX: image.Image.HeightPX,
		CapturedAtMS: now,
	}, nil
}

func (service *Service) reviewScreenshotTarget(
	ctx context.Context,
	transaction *sql.Tx,
	previewID, capability string,
) (reviewScreenshotTarget, error) {
	var target reviewScreenshotTarget
	var credentialHash []byte
	var state string
	var hardExpires int64
	err := transaction.QueryRowContext(ctx, `
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,preview.import_item_id,
	preview.source_snapshot_id,preview.validation_id,preview.provider_id,preview.target_id
FROM review_preview_sessions preview
JOIN import_items item ON item.id=preview.import_item_id AND item.state='REVIEW_PENDING'
JOIN review_drafts draft ON draft.import_item_id=item.id
 AND draft.effective_source_snapshot_id=preview.source_snapshot_id
 AND draft.target_platform_instance_id=preview.target_platform_instance_id
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
WHERE preview.id=?
	`, previewID).Scan(
		&credentialHash, &state, &hardExpires, &target.ItemID, &target.SourceSnapshotID,
		&target.ValidationID, &target.ProviderID, &target.TargetID,
	)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return reviewScreenshotTarget{}, ErrCredential
	}
	return target, nil
}

func insertReviewScreenshot(
	ctx context.Context,
	transaction *sql.Tx,
	screenshotID, previewID string,
	target reviewScreenshotTarget,
	blobID string,
	image mediaasset.Image,
	now int64,
) error {
	// A review owns one current trial result, including when older validations exist.
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM review_runtime_screenshots WHERE import_item_id=? AND validation_id<>?
`, target.ItemID, target.ValidationID); err != nil {
		return fmt.Errorf("replace prior review screenshot: %w", err)
	}
	_, err := recordstore.CreateReviewRuntimeScreenshots(ctx, transaction, `
INSERT INTO review_runtime_screenshots(id,import_item_id,preview_session_id,source_snapshot_id,
	validation_id,provider_id,target_id,blob_id,media_type,width_px,height_px,
captured_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(import_item_id,validation_id) DO UPDATE SET
id=excluded.id,preview_session_id=excluded.preview_session_id,source_snapshot_id=excluded.source_snapshot_id,
provider_id=excluded.provider_id,target_id=excluded.target_id,
	blob_id=excluded.blob_id,media_type=excluded.media_type,
width_px=excluded.width_px,height_px=excluded.height_px,
captured_at_ms=excluded.captured_at_ms,updated_at_ms=excluded.updated_at_ms
	`, screenshotID, target.ItemID, previewID, target.SourceSnapshotID, target.ValidationID,
		target.ProviderID, target.TargetID, blobID,
		image.MediaType, image.WidthPX, image.HeightPX, now, now, now)
	if err != nil {
		return fmt.Errorf("store review screenshot: %w", err)
	}
	return nil
}

func (service *Service) authorizeReviewScreenshot(ctx context.Context, previewID, capability string) error {
	var credentialHash []byte
	var state string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms
FROM review_preview_sessions WHERE id=?
`, previewID).Scan(&credentialHash, &state, &hardExpires)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return ErrCredential
	}
	return nil
}
