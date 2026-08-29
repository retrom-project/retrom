package runtimevalidation

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/mediaasset"
	retromruntime "retrom/internal/runtime"
)

type screenshotTarget struct {
	validationID, importItemID, artifactID string
}

func (service *Service) StoreRestoreScreenshot(
	ctx context.Context,
	launchID, capability string,
	reader io.Reader,
) (Screenshot, error) {
	if service.blobs == nil {
		return Screenshot{}, ErrImageInvalid
	}
	if _, err := service.authorizeScreenshot(ctx, service.database, launchID, capability); err != nil {
		return Screenshot{}, err
	}
	metadata, err := service.blobs.Put(reader)
	if err != nil {
		return Screenshot{}, ErrImageInvalid
	}
	file, err := os.Open(metadata.Path)
	if err != nil {
		return Screenshot{}, ErrImageInvalid
	}
	image, inspectErr := mediaasset.InspectImage(file, metadata.Size)
	cleanup.Error("close", file.Close())
	if inspectErr != nil || image.MediaType != "image/png" {
		return Screenshot{}, ErrImageInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Screenshot{}, fmt.Errorf("begin RPG screenshot: %w", err)
	}
	defer cleanup.Rollback(transaction)
	target, err := service.authorizeScreenshot(ctx, transaction, launchID, capability)
	if err != nil {
		return Screenshot{}, err
	}
	now := service.now().UnixMilli()
	blobID, err := blobstore.EnsureRecord(ctx, transaction, metadata, image.MediaType, now)
	if err != nil {
		return Screenshot{}, fmt.Errorf("register RPG screenshot: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE rpgmaker_runtime_validations
SET evidence_screenshot_blob_id=?,updated_at_ms=?
WHERE id=? AND state='RESTORED' AND evidence_screenshot_blob_id IS NULL
`, blobID, now, target.validationID)
	if err != nil {
		return Screenshot{}, fmt.Errorf("persist RPG screenshot: %w", err)
	}
	if count, rowsErr := result.RowsAffected(); rowsErr != nil || count != 1 {
		return Screenshot{}, ErrInvalidState
	}
	if err := transaction.Commit(); err != nil {
		return Screenshot{}, fmt.Errorf("commit RPG screenshot: %w", err)
	}
	return Screenshot{
		ValidationID: target.validationID, ImportItemID: target.importItemID, ArtifactID: target.artifactID,
		WidthPX: image.WidthPX, HeightPX: image.HeightPX, CapturedAtMS: now,
	}, nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (service *Service) authorizeScreenshot(
	ctx context.Context,
	querier rowQuerier,
	launchID, capability string,
) (screenshotTarget, error) {
	var target screenshotTarget
	var credentialHash []byte
	var launchState string
	var hardExpires, validationExpires int64
	var existing sql.NullString
	err := querier.QueryRowContext(ctx, `
SELECT validation.id,validation.import_item_id,validation.artifact_id,
 validation.expires_at_ms,validation.evidence_screenshot_blob_id,
 launch.credential_sha256,launch.state,launch.hard_expires_at_ms
FROM launch_sessions launch
JOIN rpgmaker_runtime_validations validation
 ON validation.id=launch.rpgmaker_runtime_validation_id
 AND validation.restore_launch_id=launch.id
WHERE launch.id=? AND launch.purpose='RPG_RUNTIME_VALIDATION' AND validation.state='RESTORED'
`, launchID).Scan(
		&target.validationID, &target.importItemID, &target.artifactID, &validationExpires, &existing,
		&credentialHash, &launchState, &hardExpires,
	)
	now := service.now().UnixMilli()
	if err != nil || existing.Valid || validationExpires <= now || hardExpires <= now ||
		launchState == "FINISHED" || launchState == "EXPIRED" || launchState == "REVOKED" ||
		!retromruntime.MatchesCapability(capability, credentialHash) {
		return screenshotTarget{}, ErrCredential
	}
	return target, nil
}
