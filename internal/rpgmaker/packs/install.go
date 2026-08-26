package packs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/validation"
)

type definitionIdentity struct {
	ID, Kind, Generation, DeclaredName, NormalizedName, LayoutVersion string
}

func (service *Service) Install(
	ctx context.Context,
	request InstallRequest,
) (InstallAccepted, error) {
	normalized, err := normalizeInstallRequest(request)
	if err != nil {
		return InstallAccepted{}, err
	}
	prepared, err := service.prepare(ctx, normalized)
	if err != nil {
		return InstallAccepted{}, err
	}
	installationID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return InstallAccepted{}, fmt.Errorf("runtime pack install: %w", err)
	}
	defer cleanup.Rollback(transaction)
	definition, err := service.resolveDefinition(ctx, transaction, normalized)
	if err != nil {
		return InstallAccepted{}, err
	}
	if err := ensureUnusedUpload(ctx, transaction, normalized.UploadID); err != nil {
		return InstallAccepted{}, err
	}
	if err := rejectDuplicateInstallation(ctx, transaction, definition.ID, prepared.FilesDigest); err != nil {
		return InstallAccepted{}, err
	}
	now := service.now().UnixMilli()
	bundleID, err := blobstore.EnsureRecord(ctx, transaction, prepared.Bundle, "application/zip", now)
	if err != nil {
		return InstallAccepted{}, fmt.Errorf("runtime pack bundle record: %w", err)
	}
	fileIDs, err := service.ensurePackFileRecords(ctx, transaction, prepared, now)
	if err != nil {
		return InstallAccepted{}, err
	}
	if err := insertInstallation(
		ctx, transaction, installationID.String(), definition.ID, prepared,
		bundleID, normalized, now,
	); err != nil {
		return InstallAccepted{}, err
	}
	if err := insertInstallationFiles(ctx, transaction, installationID.String(), prepared.Files, fileIDs); err != nil {
		return InstallAccepted{}, err
	}
	if err := insertInstallationJob(ctx, transaction, jobID.String(), installationID.String(), now); err != nil {
		return InstallAccepted{}, err
	}
	if err := consumeInstallationUpload(
		ctx, transaction, normalized.UploadID, installationID.String(), now,
	); err != nil {
		return InstallAccepted{}, err
	}
	if err := transaction.Commit(); err != nil {
		return InstallAccepted{}, fmt.Errorf("runtime pack install commit: %w", err)
	}
	go service.runJob(context.WithoutCancel(ctx), jobID.String())
	return InstallAccepted{
		InstallationID: installationID.String(), JobID: jobID.String(), Status: "VALIDATING",
	}, nil
}

func normalizeInstallRequest(request InstallRequest) (InstallRequest, error) {
	if _, err := uuid.Parse(request.UploadID); err != nil {
		return InstallRequest{}, ErrInvalid
	}
	if _, err := uuid.Parse(request.CreatorID); err != nil {
		return InstallRequest{}, ErrInvalid
	}
	note := ""
	if request.SourceNote != nil {
		var err error
		note, err = validation.NormalizeDecisionNote(*request.SourceNote)
		if err != nil {
			return InstallRequest{}, ErrInvalid
		}
		request.SourceNote = &note
	}
	if request.Kind == "RGSS_CUSTOM_RTP" {
		return normalizeCustomDefinitionRequest(request)
	}
	if request.Generation != nil || request.DeclaredName != nil || !builtinPackKind(request.Kind) {
		return InstallRequest{}, ErrInvalid
	}
	return request, nil
}

func normalizeCustomDefinitionRequest(request InstallRequest) (InstallRequest, error) {
	if request.Generation == nil || request.DeclaredName == nil {
		return InstallRequest{}, ErrInvalid
	}
	generation := strings.TrimSpace(*request.Generation)
	if generation != "RPGXP" && generation != "RPGVX" && generation != "RPGVXACE" {
		return InstallRequest{}, ErrInvalid
	}
	name := strings.TrimSpace(norm.NFC.String(*request.DeclaredName))
	if name == "" || len(name) > 512 || !utf8.ValidString(name) {
		return InstallRequest{}, ErrInvalid
	}
	for _, current := range name {
		if current <= 0x1f || current >= 0x7f && current <= 0x9f {
			return InstallRequest{}, ErrInvalid
		}
	}
	request.Generation = &generation
	request.DeclaredName = &name
	return request, nil
}

func builtinPackKind(kind string) bool {
	switch kind {
	case "RPG2000_RTP", "RPG2003_RTP", "RGSS1_RTP_STANDARD", "RGSS2_RTP_RPGVX", "RGSS3_RTP_RPGVXAce":
		return true
	default:
		return false
	}
}

func (service *Service) resolveDefinition(
	ctx context.Context,
	transaction *sql.Tx,
	request InstallRequest,
) (definitionIdentity, error) {
	if request.Kind != "RGSS_CUSTOM_RTP" {
		return readBuiltinDefinition(ctx, transaction, request.Kind)
	}
	normalizedName := NormalizeDeclaredName(*request.DeclaredName)
	definition, found, err := readDefinitionByName(ctx, transaction, *request.Generation, normalizedName)
	if err != nil {
		return definitionIdentity{}, err
	}
	if found {
		if definition.Kind != request.Kind {
			return definitionIdentity{}, ErrConflict
		}
		return definition, nil
	}
	id, _ := uuid.NewV7()
	definition = definitionIdentity{
		ID: "custom_" + strings.ReplaceAll(id.String(), "-", ""), Kind: request.Kind,
		Generation: *request.Generation, DeclaredName: *request.DeclaredName,
		NormalizedName: normalizedName, LayoutVersion: "mkxpz-v1",
	}
	displayName := truncateRunes(definition.DeclaredName, 200)
	_, err = transaction.ExecContext(ctx, `
INSERT INTO runtime_asset_pack_definitions(
 id,kind,generation,declared_name,normalized_declared_name,display_name,
 required_layout_version,origin,enabled,created_by_user_id,created_at_ms
) VALUES(?,?,?,?,?,?,'mkxpz-v1','CUSTOM',1,?,?)
`, definition.ID, definition.Kind, definition.Generation, definition.DeclaredName,
		definition.NormalizedName, displayName, request.CreatorID, service.now().UnixMilli())
	if err != nil {
		return definitionIdentity{}, fmt.Errorf("runtime pack custom definition: %w", err)
	}
	return definition, nil
}

func readBuiltinDefinition(
	ctx context.Context,
	transaction *sql.Tx,
	kind string,
) (definitionIdentity, error) {
	var result definitionIdentity
	err := transaction.QueryRowContext(ctx, `
SELECT id,kind,generation,declared_name,normalized_declared_name,required_layout_version
FROM runtime_asset_pack_definitions WHERE kind=? AND origin='BUILTIN' AND enabled=1
`, kind).Scan(&result.ID, &result.Kind, &result.Generation, &result.DeclaredName,
		&result.NormalizedName, &result.LayoutVersion)
	if err != nil {
		return definitionIdentity{}, ErrInvalid
	}
	return result, nil
}

func readDefinitionByName(
	ctx context.Context,
	transaction *sql.Tx,
	generation, normalizedName string,
) (definitionIdentity, bool, error) {
	var result definitionIdentity
	err := transaction.QueryRowContext(ctx, `
SELECT id,kind,generation,declared_name,normalized_declared_name,required_layout_version
FROM runtime_asset_pack_definitions WHERE generation=? AND normalized_declared_name=?
`, generation, normalizedName).Scan(&result.ID, &result.Kind, &result.Generation,
		&result.DeclaredName, &result.NormalizedName, &result.LayoutVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return definitionIdentity{}, false, nil
	}
	if err != nil {
		return definitionIdentity{}, false, fmt.Errorf("runtime pack definition lookup: %w", err)
	}
	return result, true, nil
}

func ensureUnusedUpload(ctx context.Context, transaction *sql.Tx, uploadID string) error {
	var valid int64
	err := transaction.QueryRowContext(ctx, `
SELECT count(*)=1 AND NOT EXISTS(
 SELECT 1 FROM upload_consumptions consumption WHERE consumption.upload_session_id=session.id
)
FROM upload_sessions session
WHERE session.id=? AND session.purpose='RUNTIME_ASSET_PACK' AND session.state='COMPLETE'
`, uploadID).Scan(&valid)
	if err != nil || valid != 1 {
		return ErrConflict
	}
	return nil
}

func rejectDuplicateInstallation(
	ctx context.Context,
	transaction *sql.Tx,
	definitionID, filesDigest string,
) error {
	var count int64
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM runtime_asset_pack_installations WHERE definition_id=? AND files_digest=?
`, definitionID, filesDigest).Scan(&count); err != nil {
		return fmt.Errorf("runtime pack duplicate lookup: %w", err)
	}
	if count != 0 {
		return ErrConflict
	}
	return nil
}

func (service *Service) ensurePackFileRecords(
	ctx context.Context,
	transaction *sql.Tx,
	prepared preparedInstallation,
	now int64,
) (map[string]string, error) {
	for _, metadata := range prepared.NewFileObjects {
		if _, err := blobstore.EnsureRecord(ctx, transaction, metadata, "application/octet-stream", now); err != nil {
			return nil, fmt.Errorf("runtime pack file record: %w", err)
		}
	}
	result := make(map[string]string, len(prepared.Files))
	for _, file := range prepared.Files {
		var blobID string
		err := transaction.QueryRowContext(ctx, `
SELECT id FROM blobs WHERE sha256=? AND size_bytes=?
`, file.SHA256, file.SizeBytes).Scan(&blobID)
		if err != nil {
			return nil, fmt.Errorf("runtime pack file blob: %w", err)
		}
		result[file.SHA256] = blobID
	}
	return result, nil
}

func insertInstallation(
	ctx context.Context,
	transaction *sql.Tx,
	installationID, definitionID string,
	prepared preparedInstallation,
	bundleID string,
	request InstallRequest,
	now int64,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_asset_pack_installations(
 id,definition_id,files_digest,file_count,total_bytes,bundle_blob_id,bundle_sha256,status,
 diagnostic_json,source_note,version,created_by_user_id,created_at_ms
) VALUES(?,?,?,?,?,?,?,'VALIDATING','[]',?,1,?,?)
`, installationID, definitionID, prepared.FilesDigest, len(prepared.Files), prepared.TotalBytes,
		bundleID, prepared.Bundle.SHA256, request.SourceNote, request.CreatorID, now)
	if err != nil {
		return fmt.Errorf("runtime pack installation: %w", err)
	}
	return nil
}

func insertInstallationFiles(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
	files []FileIdentity,
	fileIDs map[string]string,
) error {
	for ordinal, file := range files {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO runtime_asset_pack_files(installation_id,path,ordinal,blob_id,size_bytes,sha256)
VALUES(?,?,?,?,?,?)
`, installationID, file.Path, ordinal, fileIDs[file.SHA256], file.SizeBytes, file.SHA256)
		if err != nil {
			return fmt.Errorf("runtime pack installation file: %w", err)
		}
	}
	return nil
}

func insertInstallationJob(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, installationID string,
	now int64,
) error {
	payload, _ := json.Marshal(map[string]any{
		"installationId": installationID, "schemaVersion": 1,
	})
	digest := inputDigest(payload)
	_, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'RUNTIME_ASSET_PACK_INSTALLATION',?,'RUNTIME_ASSET_PACK_VALIDATE',?,1,?,0,'QUEUED',0,1,?,?,?)
`, jobID, installationID, inputDigest([]byte(installationID)), string(payload), now, now, now)
	if err != nil {
		return fmt.Errorf("runtime pack validation job: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, jobID, string(payload), digest, now)
	if err != nil {
		return fmt.Errorf("runtime pack validation job input: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'RUNTIME_ASSET_PACK_INSTALLATION',?,'QUEUED','{}',?)
`, jobID, installationID, now)
	if err != nil {
		return fmt.Errorf("runtime pack validation job event: %w", err)
	}
	return nil
}

func consumeInstallationUpload(
	ctx context.Context,
	transaction *sql.Tx,
	uploadID, installationID string,
	now int64,
) error {
	id, _ := uuid.NewV7()
	_, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(
 id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms
) VALUES(?,?,NULL,'RUNTIME_ASSET_PACK_INSTALLATION',?,?)
`, id.String(), uploadID, installationID, now)
	if err != nil {
		return fmt.Errorf("runtime pack upload consumption: %w", err)
	}
	return nil
}

func truncateRunes(value string, maximum int) string {
	values := []rune(value)
	if len(values) <= maximum {
		return value
	}
	return string(values[:maximum])
}
