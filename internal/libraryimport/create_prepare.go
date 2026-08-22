package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
)

type creationTarget struct {
	platformID          string
	coreID              string
	artifactID          string
	emulatorVersion     string
	artifactPath        string
	artifactSHA         string
	compatibilityConfig string
	instanceVersion     int64
	artifactVersion     int64
}

type creationPlan struct {
	request      CreateRequest
	contentMode  string
	sourceType   string
	target       creationTarget
	datID        sql.NullString
	files        []importSourceFile
	dispositions []preparedDisposition
	groups       []preparedGroup
	archives     []preparedArchive
}

func normalizeCreateRequest(request CreateRequest) (CreateRequest, string, error) {
	if request.TagIDs == nil {
		request.TagIDs = []string{}
	}
	contentMode := request.ContentMode
	if contentMode == "" {
		contentMode = contentcapability.ModeStandard
	}
	if contentMode != contentcapability.ModeStandard && contentMode != contentcapability.ModeMultiDiscM3UV1 {
		return CreateRequest{}, "", ErrInvalid
	}
	if request.MetadataProvider != "NONE" && request.MetadataProvider != "HASHEOUS" {
		return CreateRequest{}, "", ErrInvalid
	}
	return request, contentMode, nil
}

func (service *Service) prepareCreation(ctx context.Context, rawRequest CreateRequest) (creationPlan, error) {
	request, contentMode, err := normalizeCreateRequest(rawRequest)
	if err != nil {
		return creationPlan{}, err
	}
	if request.MetadataProvider == "HASHEOUS" && service.scraper == nil {
		return creationPlan{}, fmt.Errorf("libraryimport/service: %w", errMetadataScraperNotConfigured)
	}
	sourceType, err := service.loadCompletedUpload(ctx, request.UploadID)
	if err != nil {
		return creationPlan{}, err
	}
	if contentMode == contentcapability.ModeMultiDiscM3UV1 && sourceType != "DIRECTORY" {
		return creationPlan{}, ErrMultiDiscModeUnavailable
	}
	target, err := service.loadCreationTarget(ctx, request.TargetPlatformInstanceID)
	if err != nil {
		return creationPlan{}, err
	}
	capabilities := contentcapability.Resolve(
		target.platformID, true, service.multiDiscImportEnabled, target.compatibilityConfig,
	)
	if contentMode == contentcapability.ModeMultiDiscM3UV1 && capabilities.MultiDisc == nil {
		return creationPlan{}, ErrMultiDiscModeUnavailable
	}
	datID := service.loadActiveDATID(ctx, target.artifactID)
	files, err := service.loadImportSourceFiles(ctx, request.UploadID)
	if err != nil {
		return creationPlan{}, err
	}
	plan := creationPlan{
		request: request, contentMode: contentMode, sourceType: sourceType,
		target: target, datID: datID, files: files,
	}
	if contentMode == contentcapability.ModeMultiDiscM3UV1 {
		plan.dispositions, plan.groups, err = service.prepareMultiDiscFiles(files, *capabilities.MultiDisc)
	} else {
		plan.dispositions, plan.groups, plan.archives = service.prepareImportFiles(
			ctx, target.platformID, sourceType, files, datID,
		)
	}
	if err != nil {
		return creationPlan{}, err
	}
	return plan, nil
}

func (service *Service) loadCompletedUpload(ctx context.Context, uploadID string) (string, error) {
	var state, sourceType string
	err := service.database.QueryRowContext(ctx, `
SELECT state,source_type
FROM upload_sessions
WHERE id=?
`, uploadID).Scan(&state, &sourceType)
	if err != nil || state != "COMPLETE" {
		return "", ErrInvalid
	}
	return sourceType, nil
}

func (service *Service) loadCreationTarget(ctx context.Context, instanceID string) (creationTarget, error) {
	var target creationTarget
	err := service.database.QueryRowContext(ctx, `
SELECT pi.platform_id,pi.default_core_id,pi.version,a.id,a.emulatorjs_version,a.relative_path,
a.sha256,a.version,a.compatibility_config_json
FROM platform_instances pi
JOIN core_artifacts a ON a.core_id=pi.default_core_id AND a.enabled=1
WHERE pi.id=? AND pi.enabled=1 AND pi.deleted_at_ms IS NULL
`, instanceID).Scan(
		&target.platformID, &target.coreID, &target.instanceVersion, &target.artifactID,
		&target.emulatorVersion, &target.artifactPath, &target.artifactSHA,
		&target.artifactVersion, &target.compatibilityConfig,
	)
	if err != nil {
		return creationTarget{}, ErrInvalid
	}
	return target, nil
}

func (service *Service) loadActiveDATID(ctx context.Context, artifactID string) sql.NullString {
	var datID sql.NullString
	_ = service.database.QueryRowContext(ctx, `
SELECT id FROM dat_versions WHERE core_artifact_id=? AND is_active=1
`, artifactID).Scan(&datID)
	return datID
}

func (service *Service) loadImportSourceFiles(ctx context.Context, uploadID string) ([]importSourceFile, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT f.id,f.relative_path,f.final_blob_id,b.sha256,b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.upload_session_id=? AND f.state='COMPLETE'
ORDER BY f.relative_path,f.id
`, uploadID)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var files []importSourceFile
	for rows.Next() {
		var file importSourceFile
		if err := rows.Scan(&file.id, &file.path, &file.blobID, &file.sha256, &file.size); err != nil {
			return nil, fmt.Errorf("libraryimport/service: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	if len(files) == 0 {
		return nil, ErrInvalid
	}
	return files, nil
}
