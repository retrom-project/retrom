package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	librarypersistence "retrom/internal/persistence/libraryimport"

	"github.com/google/uuid"

	"retrom/internal/contentcapability"
)

type preparedServerSource struct {
	uploadID, sourceType, manifestDigest, contentMode string
	files                                             []reusableUploadFile
	totalBytes                                        int64
	idempotent                                        bool
	ownerKind, ownerItemID                            string
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
	ownerKind, ownerItemID, _ := strings.Cut(idempotencyKey, ":")
	if ownerKind == "IMPORT_RECEIVE" {
		ownerKind = "SOURCE"
	}
	if ownerKind != "SOURCE" {
		ownerKind, ownerItemID = "", ""
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
		ownerKind: ownerKind, ownerItemID: ownerItemID,
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
		prepared.ownerKind, prepared.ownerItemID,
	)
}

func (service *Service) serverSourceUploadPresent(
	ctx context.Context,
	uploadID, sourceType, manifestDigest string,
	totalBytes int64,
	totalFiles int,
) (bool, error) {
	present, err := librarypersistence.NewServerSourceUploads(service.database).Present(
		ctx, uploadID, sourceType, manifestDigest, totalBytes, totalFiles,
	)
	if err != nil {
		return false, fmt.Errorf("check reusable server upload: %w", err)
	}
	return present, nil
}

func (service *Service) serverSourceCreation(
	ctx context.Context,
	uploadID, targetPlatformInstanceID, contentMode string,
) (Created, bool, error) {
	created, found, err := librarypersistence.NewServerSourceUploads(service.database).Creation(
		ctx, uploadID, targetPlatformInstanceID, contentMode,
	)
	if err != nil {
		return Created{}, false, fmt.Errorf("read reusable server import: %w", err)
	}
	return created, found, nil
}
