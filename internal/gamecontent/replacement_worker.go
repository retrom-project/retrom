package gamecontent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/multidisc"
	"retrom/internal/payloadrelease"
)

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) run(parent context.Context, jobID string, snapshot jobSnapshot) {
	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='RUNNING',
attempt_count=attempt_count+1,
execution_started_at_ms=?,
execution_deadline_at_ms=?,
leased_until_ms=?,
heartbeat_at_ms=?,
worker_id='in-process',
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`,
		now,
		now+300_000,
		now+60_000,
		now,
		now,
		jobID,
	)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return
	}
	_, _ = service.database.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'GAME',
?,
'STARTED',
'{}',
?)
`,
		jobID,
		snapshot.GameID,
		now,
	)
	service.runPreparedReplacement(ctx, jobID, snapshot, now)
}

func (service *Service) runPreparedReplacement(
	ctx context.Context,
	jobID string,
	snapshot jobSnapshot,
	now int64,
) {
	uploadID := snapshot.UploadSessionID
	files, err := collectUploadFiles(ctx, service.database, uploadID)
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_INPUT_UNAVAILABLE")
		return
	}
	prepared, err := service.prepareReplacement(ctx, snapshot, files)
	if err != nil {
		var validationErr *replacementValidationError
		if errors.As(err, &validationErr) {
			service.failTerminal(ctx, jobID, validationErr.code)
		} else {
			service.fail(ctx, jobID, "GAME_CONTENT_INPUT_UNAVAILABLE")
		}
		return
	}
	if prepared.rpgMaker != nil {
		service.persistRPGMakerReplacement(ctx, jobID, snapshot, prepared, now)
		return
	}
	biosSnapshot, biosStatus, biosCode, err := corevalidation.ResolveBIOS(
		ctx, service.database, snapshot.ProviderID, snapshot.TargetID, prepared.firstContentLogicalName,
	)
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_INPUT_UNAVAILABLE")
		return
	}
	if biosStatus != "READY" {
		service.fail(ctx, jobID, biosCode)
		return
	}
	if prepared.contentKind == multidisc.ContentKind {
		biosSnapshot.MultiDisc = &corevalidation.MultiDiscSnapshot{
			ContentKind:             corevalidation.MultiDiscContentKind,
			ParserVersion:           corevalidation.MultiDiscParserVersion,
			DiscCount:               len(prepared.orderedDiscSHA256),
			MissingEntries:          []corevalidation.MultiDiscMissingEntry{},
			OrderedDiscSHA256:       prepared.orderedDiscSHA256,
			CanonicalPlaylistSHA256: prepared.canonicalPlaylist.SHA256,
			Delivery:                corevalidation.MultiDiscDelivery,
		}
	}
	dependencySnapshotJSON, err := biosSnapshot.JSON()
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_INPUT_UNAVAILABLE")
		return
	}
	service.persistPreparedReplacement(
		ctx, jobID, snapshot, prepared, biosSnapshot, dependencySnapshotJSON, now,
	)
}

func (service *Service) persistPreparedReplacement(
	ctx context.Context,
	jobID string,
	snapshot jobSnapshot,
	prepared preparedReplacement,
	_ corevalidation.Snapshot,
	dependencySnapshotJSON []byte,
	now int64,
) {
	gameID := snapshot.GameID
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_DATABASE_FAILED")
		return
	}
	defer cleanup.Rollback(transaction)
	failTransaction := func(code string) {
		cleanup.Rollback(transaction)
		service.fail(ctx, jobID, code)
	}
	currentBinding, err := loadReplacementBinding(ctx, transaction, gameID)
	if err != nil || !replacementBindingMatchesSnapshot(currentBinding, snapshot) ||
		currentBinding.contentPolicy.Digest() != snapshot.TargetPolicyDigest ||
		nullableText(currentBinding.datID) != pointerText(snapshot.DATVersionID) {
		cleanup.Rollback(transaction)
		service.failTerminal(ctx, jobID, "GAME_CONTENT_CHANGED")
		return
	}
	unchanged, err := contentReplacementUnchanged(ctx, transaction, gameID, prepared)
	if err != nil {
		cleanup.Rollback(transaction)
		service.fail(ctx, jobID, "GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if unchanged {
		cleanup.Rollback(transaction)
		service.failUnchanged(ctx, jobID)
		return
	}
	if service.payloadReleases == nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	impact, err := service.payloadReleases.RetireCurrentGameContent(
		ctx, transaction, gameID, snapshot.VariantID, now,
	)
	if err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	service.writeReplacement(
		ctx, transaction, jobID, snapshot, prepared, dependencySnapshotJSON, impact, now, failTransaction,
	)
}

func (service *Service) writeReplacement(
	ctx context.Context,
	transaction *sql.Tx,
	jobID string,
	snapshot jobSnapshot,
	prepared preparedReplacement,
	dependencySnapshotJSON []byte,
	impact payloadrelease.ContentReplacementImpact,
	now int64,
	failTransaction func(string),
) {
	gameID := snapshot.GameID
	gameResult, err := transaction.ExecContext(ctx, `
UPDATE games SET content_kind=?,content_source_kind='ADMIN_REPLACE',content_source_ref_id=?,
 source_manifest_json=?,source_manifest_digest=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=? AND source_manifest_digest=? AND status='PUBLISHED'
`, prepared.contentKind, jobID, string(prepared.manifest), prepared.manifestDigest, now,
		gameID, snapshot.GameVersion, snapshot.BaseManifestDigest)
	if err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if changed, _ := gameResult.RowsAffected(); changed != 1 {
		failTransaction("GAME_CONTENT_CHANGED")
		return
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM game_files WHERE game_id=?`, gameID); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := persistReplacementFiles(ctx, transaction, gameID, prepared.files); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if _, err := transaction.ExecContext(
		ctx, `DELETE FROM variant_files WHERE game_variant_id=?`, snapshot.VariantID,
	); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if _, err := transaction.ExecContext(
		ctx, `DELETE FROM variant_dependencies WHERE game_variant_id=?`, snapshot.VariantID,
	); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := service.attachReplacementPlaylist(ctx, transaction, snapshot.VariantID, prepared, now); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	variantResult, err := transaction.ExecContext(ctx, `
UPDATE game_variants SET provider_id=?,target_id=?,dat_version_id=?,status='READY',
 compatibility_code='READY',dependency_snapshot_json=?,version=version+1,updated_at_ms=?
WHERE id=? AND game_id=?
`, snapshot.ProviderID, snapshot.TargetID, nullableValue(snapshot.DATVersionID),
		string(dependencySnapshotJSON), now, snapshot.VariantID, gameID)
	if err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if changed, _ := variantResult.RowsAffected(); changed != 1 {
		failTransaction("GAME_CONTENT_CHANGED")
		return
	}
	if err := service.payloadReleases.StageCandidates(ctx, transaction, impact.CandidateBlobIDs); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	service.finishReplacement(
		ctx, transaction, jobID, snapshot, prepared.manifestDigest, impact.SaveStateCount,
		failTransaction,
	)
}

func (service *Service) finishReplacement(
	ctx context.Context,
	transaction *sql.Tx,
	jobID string,
	snapshot jobSnapshot,
	manifestDigest string,
	retiredSaveStateCount int64,
	failTransaction func(string),
) {
	gameID := snapshot.GameID
	finished := service.now().UnixMilli()
	jobResult, err := transaction.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='RUNNING'
AND worker_id='in-process'
`,
		finished,
		finished,
		jobID,
	)
	if err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if changed, _ := jobResult.RowsAffected(); changed != 1 {
		cleanup.Rollback(transaction)
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'GAME',
?,
'SUCCEEDED',
?,
?)
`,
		jobID,
		gameID,
		fmt.Sprintf(`{"gameId":%q,"gameVariantId":%q,"manifestDigest":%q,"retiredSaveStateCount":%d}`,
			gameID, snapshot.VariantID, manifestDigest, retiredSaveStateCount),
		finished,
	); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	var consumptionID string
	if err := transaction.QueryRowContext(ctx, `
	SELECT id FROM upload_consumptions WHERE consumer_type='GAME_CONTENT_REPLACE_JOB' AND consumer_id=?
`, jobID).Scan(&consumptionID); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if _, err := payloadrelease.ScheduleConsumption(ctx, transaction, consumptionID, finished); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := transaction.Commit(); err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if service.payloadReleases != nil {
		service.payloadReleases.Signal()
	}
}

func persistReplacementFiles(
	ctx context.Context,
	transaction *sql.Tx,
	contentID string,
	files []replacementFile,
) error {
	for _, value := range files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_files(game_id,
role,
logical_name,
blob_id,
sort_order) VALUES(?,
?,
?,
?,
?)
`, contentID, value.role, value.logicalName, value.blobID, value.sortOrder); err != nil {
			return fmt.Errorf("insert replacement content file: %w", err)
		}
	}
	return nil
}

func (service *Service) attachReplacementPlaylist(
	ctx context.Context,
	transaction *sql.Tx,
	variantID string,
	prepared preparedReplacement,
	now int64,
) error {
	if prepared.contentKind != multidisc.ContentKind {
		return nil
	}
	playlistBlobID, err := blobstore.EnsureRecord(
		ctx, transaction, prepared.canonicalPlaylist, "application/vnd.retrom.m3u", now,
	)
	if err != nil {
		return fmt.Errorf("register replacement playlist: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
VALUES(?,'MULTI_DISC_PLAYLIST','playlist.m3u',?,0)
`, variantID, playlistBlobID); err != nil {
		return fmt.Errorf("attach replacement playlist: %w", err)
	}
	return nil
}
