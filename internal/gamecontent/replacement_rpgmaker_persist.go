package gamecontent

import (
	"context"
	"database/sql"
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
		ctx, transaction, snapshot.GameID, prepared,
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
	if service.payloadReleases == nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	impact, err := service.payloadReleases.RetireCurrentGameContent(
		ctx, transaction, snapshot.GameID, snapshot.VariantID, now,
	)
	if err != nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := persistRPGMakerReplacementContent(
		ctx, transaction, jobID, snapshot.GameID, prepared, now,
	); err != nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := service.persistRPGMakerReplacementVariant(
		ctx, transaction, snapshot, prepared, binding, snapshot.VariantID, now,
	); err != nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := service.payloadReleases.StageCandidates(ctx, transaction, impact.CandidateBlobIDs); err != nil {
		fail("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	service.finishReplacement(
		ctx, transaction, jobID, snapshot, prepared.manifestDigest, impact.SaveStateCount, fail,
	)
}

func persistRPGMakerReplacementContent(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, gameID string,
	prepared preparedReplacement,
	now int64,
) error {
	profile := prepared.rpgMaker
	if profile == nil {
		return fmt.Errorf("%w: missing RPG replacement profile", ErrInvalid)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE games SET content_kind=?,content_source_kind='ADMIN_REPLACE',content_source_ref_id=?,
 source_manifest_json=?,source_manifest_digest=?,version=version+1,updated_at_ms=?
WHERE id=?
`, prepared.contentKind, jobID, string(prepared.manifest), prepared.manifestDigest, now, gameID); err != nil {
		return fmt.Errorf("update RPG replacement content: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM game_files WHERE game_id=?`, gameID); err != nil {
		return fmt.Errorf("delete RPG replacement files: %w", err)
	}
	if err := persistReplacementFiles(ctx, transaction, gameID, prepared.files); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM rpgmaker_game_profiles WHERE game_id=?`, gameID); err != nil {
		return fmt.Errorf("delete RPG replacement profile: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO rpgmaker_game_profiles(
 game_id,evidence_family,evidence_generation,evidence_confidence,engine_version,
 entry_html_path,file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,
 created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
`, gameID, profile.profile.EvidenceFamily, rpgReplacementEvidenceGeneration(profile.profile),
		profile.profile.EvidenceConfidence, nullableRPGReplacementString(profile.profile.EngineVersion),
		rpgReplacementEntryHTML(profile.profile.ExpectedGeneration), profile.fileCount, profile.totalBytes,
		profile.projectFingerprint, profile.requirementsSHA256, string(profile.analysisJSON), now, now); err != nil {
		return fmt.Errorf("insert RPG replacement content profile: %w", err)
	}
	return nil
}

func (service *Service) persistRPGMakerReplacementVariant(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot jobSnapshot,
	prepared preparedReplacement,
	binding replacementBinding,
	variantID string,
	now int64,
) error {
	profile := prepared.rpgMaker
	if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants SET provider_id=?,target_id=?,dat_version_id=NULL,status='READY',
 compatibility_code='READY',dependency_snapshot_json=?,version=version+1,updated_at_ms=?
WHERE id=? AND game_id=?
`, snapshot.ProviderID, snapshot.TargetID, binding.dependencySnapshotJSON, now,
		variantID, snapshot.GameID); err != nil {
		return fmt.Errorf("update RPG replacement variant: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE rpgmaker_variant_profiles SET generation=?,dependency_snapshot_sha256=?
WHERE game_variant_id=?
`, snapshot.RPGGeneration, snapshot.RPGDependencySHA256, variantID); err != nil {
		return fmt.Errorf("update RPG replacement variant profile: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM variant_files WHERE game_variant_id=?`, variantID); err != nil {
		return fmt.Errorf("delete RPG replacement variant files: %w", err)
	}
	if err := service.persistRPGMakerReplacementVariantFiles(
		ctx, transaction, variantID, profile.variantFiles, now,
	); err != nil {
		return err
	}
	return nil
}

func (service *Service) persistRPGMakerReplacementVariantFiles(
	ctx context.Context,
	transaction *sql.Tx,
	variantID string,
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
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
VALUES(?,?,?,?,?)
`, variantID, file.role, file.logicalName, blobID, index); err != nil {
			return fmt.Errorf("attach RPG replacement variant file: %w", err)
		}
	}
	return nil
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
