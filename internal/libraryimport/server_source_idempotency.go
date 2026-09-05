package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/contentcapability"
)

type preparedServerSource struct {
	uploadID, sourceType, manifestDigest, contentMode string
	files                                             []reusableUploadFile
	totalBytes                                        int64
	idempotent                                        bool
}

func (prepared preparedServerSource) reviewHandoffKind() string {
	if prepared.idempotent {
		return reviewHandoffEmulationStation
	}
	return reviewHandoffDirect
}

func (service *Service) prepareServerSource(
	ctx context.Context,
	idempotencyKey, contentMode string,
	files []ServerSourceFile,
) (preparedServerSource, error) {
	if len(files) == 0 || len(files) > ServerSourceFileLimit {
		return preparedServerSource{}, ErrInvalid
	}
	if contentMode == "" {
		contentMode = contentcapability.ModeStandard
	}
	sorted, reusable, totalBytes, err := service.validateServerFiles(ctx, files)
	if err != nil {
		return preparedServerSource{}, err
	}
	uploadID, _ := uuid.NewV7()
	if idempotencyKey != "" {
		uploadID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("retrom:server-source:v1\x00"+idempotencyKey))
	}
	manifest, _ := json.Marshal(map[string]any{"schemaVersion": 1, "files": sorted})
	digest := sha256.Sum256(manifest)
	sourceType := "FILES"
	if contentMode == contentcapability.ModeMultiDisc {
		sourceType = "DIRECTORY"
	}
	return preparedServerSource{
		uploadID: uploadID.String(), sourceType: sourceType,
		manifestDigest: hex.EncodeToString(digest[:]), contentMode: contentMode,
		files: reusable, totalBytes: totalBytes, idempotent: idempotencyKey != "",
	}, nil
}

func (service *Service) ensureServerSourceUpload(
	ctx context.Context,
	prepared preparedServerSource,
	targetPlatformInstanceID string,
) (Created, bool, error) {
	if !prepared.idempotent {
		err := service.insertPreparedServerUpload(ctx, prepared)
		return Created{}, false, err
	}
	present, err := service.serverSourceUploadPresent(
		ctx, prepared.uploadID, prepared.sourceType, prepared.manifestDigest,
		prepared.totalBytes, len(prepared.files),
	)
	if err != nil {
		return Created{}, false, err
	}
	if !present {
		if insertErr := service.insertPreparedServerUpload(ctx, prepared); insertErr != nil {
			present, err = service.serverSourceUploadPresent(
				ctx, prepared.uploadID, prepared.sourceType, prepared.manifestDigest,
				prepared.totalBytes, len(prepared.files),
			)
			if err != nil || !present {
				return Created{}, false, insertErr
			}
		}
	}
	return service.serverSourceCreation(
		ctx, prepared.uploadID, targetPlatformInstanceID, prepared.contentMode,
	)
}

func (service *Service) insertPreparedServerUpload(
	ctx context.Context,
	prepared preparedServerSource,
) error {
	return service.insertServerUpload(
		ctx,
		prepared.uploadID,
		prepared.sourceType,
		prepared.files,
		prepared.manifestDigest,
		service.now().UnixMilli(),
		prepared.totalBytes,
	)
}

func (service *Service) serverSourceUploadPresent(
	ctx context.Context,
	uploadID, sourceType, manifestDigest string,
	totalBytes int64,
	totalFiles int,
) (bool, error) {
	var state, storedSourceType, storedDigest string
	var storedFiles int
	var storedBytes int64
	err := service.database.QueryRowContext(ctx, `
SELECT state,source_type,total_files,total_bytes,manifest_digest
FROM upload_sessions
WHERE id=?
`, uploadID).Scan(&state, &storedSourceType, &storedFiles, &storedBytes, &storedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("libraryimport/server source idempotency: %w", err)
	}
	if state != "COMPLETE" || storedSourceType != sourceType || storedFiles != totalFiles ||
		storedBytes != totalBytes || storedDigest != manifestDigest {
		return false, ErrInvalid
	}
	return true, nil
}

func (service *Service) serverSourceCreation(
	ctx context.Context,
	uploadID, targetPlatformInstanceID, contentMode string,
) (Created, bool, error) {
	var created Created
	err := service.database.QueryRowContext(ctx, `
SELECT import.id,job.id,import.state,import.total_item_count
FROM import_jobs import
JOIN jobs job ON job.scope_type='IMPORT_GROUP' AND job.scope_id=import.id AND job.kind='IMPORT_GROUP'
WHERE import.upload_session_id=?
AND import.target_platform_instance_id=?
AND json_extract(import.config_snapshot_json,'$.contentMode')=?
`, uploadID, targetPlatformInstanceID, contentMode).Scan(
		&created.ImportJobID,
		&created.JobID,
		&created.State,
		&created.ItemCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if countErr := service.database.QueryRowContext(
			ctx, `SELECT count(*) FROM import_jobs WHERE upload_session_id=?`, uploadID,
		).Scan(&count); countErr != nil {
			return Created{}, false, fmt.Errorf("libraryimport/server source idempotency: %w", countErr)
		}
		if count != 0 {
			return Created{}, false, ErrInvalid
		}
		return Created{}, false, nil
	}
	if err != nil {
		return Created{}, false, fmt.Errorf("libraryimport/server source idempotency: %w", err)
	}
	return created, true, nil
}
