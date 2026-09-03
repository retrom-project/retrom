package gamecontent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/detector"
)

func (service *Service) persistRPGMakerReplacement(
	ctx context.Context,
	jobID string,
	snapshot jobSnapshot,
	prepared preparedReplacement,
	now int64,
) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_DATABASE_FAILED")
		return
	}
	defer cleanup.Rollback(transaction)
	fail := func(code string) {
		cleanup.Rollback(transaction)
		service.fail(ctx, jobID, code)
	}
	binding, err := loadReplacementBinding(ctx, transaction, snapshot.GameID)
	if err != nil || !replacementBindingMatchesSnapshot(binding, snapshot) {
		cleanup.Rollback(transaction)
		service.fail(ctx, jobID, "GAME_CONTENT_SNAPSHOT_STALE")
		return
	}
	unchanged, err := contentReplacementUnchanged(
		ctx, transaction, snapshot.BaseContentRevisionID, prepared,
	)
	if err != nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if unchanged {
		cleanup.Rollback(transaction)
		service.failUnchanged(ctx, jobID)
		return
	}
	contentID, err := persistRPGMakerReplacementContent(
		ctx, transaction, jobID, snapshot.GameID, prepared, now,
	)
	if err != nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	variantID, err := ensureReplacementVariant(ctx, transaction, snapshot.GameID, snapshot.CoreID, now)
	if err != nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	revisionID, err := service.persistRPGMakerReplacementVariant(
		ctx, transaction, snapshot, prepared, binding, contentID, variantID, now,
	)
	if err != nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	service.finishReplacement(
		ctx, transaction, jobID, snapshot, contentID, variantID, revisionID, now, fail,
	)
}

func persistRPGMakerReplacementContent(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, gameID string,
	prepared preparedReplacement,
	now int64,
) (string, error) {
	profile := prepared.rpgMaker
	if profile == nil {
		return "", fmt.Errorf("%w: missing RPG replacement profile", ErrInvalid)
	}
	contentID := newID()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_content_revisions(id,game_id,content_kind,source_kind,source_ref_id,
source_manifest_json,source_manifest_digest,created_at_ms)
VALUES(?,?,?,'ADMIN_REPLACE',?,?,?,?)
`, contentID, gameID, prepared.contentKind, jobID,
		string(prepared.manifest), prepared.manifestDigest, now); err != nil {
		return "", fmt.Errorf("insert RPG replacement content: %w", err)
	}
	if err := persistReplacementFiles(ctx, transaction, contentID, prepared.files); err != nil {
		return "", err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO rpgmaker_content_profiles(
 content_revision_id,evidence_family,evidence_generation,evidence_confidence,engine_version,
 entry_html_path,file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
`, contentID, profile.profile.EvidenceFamily, rpgReplacementEvidenceGeneration(profile.profile),
		profile.profile.EvidenceConfidence, nullableRPGReplacementString(profile.profile.EngineVersion),
		rpgReplacementEntryHTML(profile.profile.ExpectedGeneration), profile.fileCount, profile.totalBytes,
		profile.projectFingerprint, profile.requirementsSHA256, string(profile.analysisJSON), now); err != nil {
		return "", fmt.Errorf("insert RPG replacement content profile: %w", err)
	}
	return contentID, nil
}

func (service *Service) persistRPGMakerReplacementVariant(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot jobSnapshot,
	prepared preparedReplacement,
	binding replacementBinding,
	contentID, variantID string,
	now int64,
) (string, error) {
	profile := prepared.rpgMaker
	revisionID := newID()
	inputDigest := rpgReplacementValidationDigest(snapshot, profile.projectFingerprint, contentID, variantID)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_variant_revisions(
 id,game_variant_id,game_content_revision_id,provider_id,target_id,target_contract_sha256,
 game_compatibility_line,dat_version_id,
 validation_input_digest,emulator_game_id,status,compatibility_code,dependency_snapshot_json,created_at_ms
) VALUES(?,?,?,?,?,?,?,NULL,?,NULL,'READY','READY',?,?)
`, revisionID, variantID, contentID, snapshot.ProviderID, snapshot.TargetID,
		snapshot.TargetContractSHA256, snapshot.GameCompatibilityLine,
		inputDigest, binding.dependencySnapshotJSON, now); err != nil {
		return "", fmt.Errorf("insert RPG replacement variant: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO rpgmaker_variant_profiles(
 game_variant_revision_id,generation,dependency_snapshot_sha256,runtime_validation_id
) VALUES(?,?,?,NULL)
`, revisionID, snapshot.RPGGeneration, snapshot.RPGDependencySHA256); err != nil {
		return "", fmt.Errorf("insert RPG replacement variant profile: %w", err)
	}
	if err := service.persistRPGMakerReplacementVariantFiles(
		ctx, transaction, revisionID, profile.variantFiles, now,
	); err != nil {
		return "", err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_variant_revision_runtime_packs(
 game_variant_revision_id,slot,declared_name,normalized_declared_name,definition_id,installation_id
)
SELECT ?,slot,declared_name,normalized_declared_name,definition_id,installation_id
FROM game_variant_revision_runtime_packs WHERE game_variant_revision_id=? ORDER BY slot
`, revisionID, snapshot.BaseVariantRevisionID); err != nil {
		return "", fmt.Errorf("copy RPG replacement runtime packs: %w", err)
	}
	return revisionID, nil
}

func (service *Service) persistRPGMakerReplacementVariantFiles(
	ctx context.Context,
	transaction *sql.Tx,
	revisionID string,
	files []preparedRPGMakerVariantFile,
	now int64,
) error {
	for index, file := range files {
		blobID, err := blobstore.EnsureRecord(
			ctx, transaction, file.metadata, "application/octet-stream", now,
		)
		if err != nil {
			return fmt.Errorf("register RPG replacement variant file: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,?,?,?,?)
`, revisionID, file.role, file.logicalName, blobID, index); err != nil {
			return fmt.Errorf("attach RPG replacement variant file: %w", err)
		}
	}
	return nil
}

func rpgReplacementValidationDigest(
	snapshot jobSnapshot,
	projectFingerprint, contentID, variantID string,
) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf(
		"RETROM_RPG_REPLACEMENT_V2\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		variantID, contentID, projectFingerprint, snapshot.ProviderID,
		snapshot.TargetID, snapshot.TargetContractSHA256, snapshot.RPGDependencySHA256,
	)))
	return hex.EncodeToString(digest[:])
}

func rpgReplacementEvidenceGeneration(profile detector.Profile) any {
	if profile.EvidenceGeneration == nil {
		return nil
	}
	return string(*profile.EvidenceGeneration)
}

func rpgReplacementEntryHTML(generation detector.Generation) any {
	if generation == detector.RPGMV || generation == detector.RPGMZ {
		return "index.html"
	}
	return nil
}
