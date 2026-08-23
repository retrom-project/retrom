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
	artifactID := snapshot.CoreArtifactID
	files, err := collectUploadFiles(ctx, service.database, uploadID)
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_INPUT_UNAVAILABLE")
		return
	}
	prepared, err := service.prepareReplacement(ctx, snapshot, files)
	if err != nil {
		var validationErr *replacementValidationError
		if errors.As(err, &validationErr) {
			service.fail(ctx, jobID, validationErr.code)
		} else {
			service.fail(ctx, jobID, "GAME_CONTENT_INPUT_UNAVAILABLE")
		}
		return
	}
	biosSnapshot, biosStatus, biosCode, err := corevalidation.ResolveBIOS(
		ctx, service.database, artifactID, prepared.firstContentLogicalName,
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
	biosSnapshot corevalidation.Snapshot,
	dependencySnapshotJSON []byte,
	now int64,
) {
	gameID := snapshot.GameID
	previousContentID := snapshot.BaseContentRevisionID
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
	var currentContent string
	var currentVersion int64
	var currentInstance, currentArtifact, currentCompatibilityJSON string
	var currentPlatformVersion, currentArtifactVersion int64
	var currentDAT sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT g.current_content_revision_id,
g.version,
g.platform_instance_id,
	pi.version,
	a.id,
	a.version,
	a.compatibility_config_json,
	(SELECT id
FROM dat_versions
WHERE core_artifact_id=a.id
AND is_active=1)
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN core_artifacts a ON a.core_id=pi.default_core_id
AND a.enabled=1
WHERE g.id=?
AND g.status='PUBLISHED'
`, gameID).Scan(
		&currentContent,
		&currentVersion,
		&currentInstance,
		&currentPlatformVersion,
		&currentArtifact,
		&currentArtifactVersion,
		&currentCompatibilityJSON,
		&currentDAT,
	); err != nil ||
		currentContent != previousContentID ||
		currentVersion != snapshot.GameVersion ||
		currentInstance != snapshot.PlatformInstanceID ||
		currentPlatformVersion != snapshot.PlatformInstanceVersion ||
		currentArtifact != snapshot.CoreArtifactID ||
		currentArtifactVersion != snapshot.CoreArtifactVersion ||
		corevalidation.CompatibilityConfigDigest(currentCompatibilityJSON) != snapshot.CompatibilityConfigDigest ||
		nullableText(currentDAT) != pointerText(snapshot.DATVersionID) {
		cleanup.Rollback(transaction)
		service.fail(ctx, jobID, "GAME_CONTENT_SNAPSHOT_STALE")
		return
	}
	unchanged, err := contentReplacementUnchanged(ctx, transaction, currentContent, prepared)
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
	service.writeReplacement(
		ctx, transaction, jobID, snapshot, prepared, biosSnapshot,
		dependencySnapshotJSON, currentDAT, now, failTransaction,
	)
}

func (service *Service) writeReplacement(
	ctx context.Context,
	transaction *sql.Tx,
	jobID string,
	snapshot jobSnapshot,
	prepared preparedReplacement,
	biosSnapshot corevalidation.Snapshot,
	dependencySnapshotJSON []byte,
	currentDAT sql.NullString,
	now int64,
	failTransaction func(string),
) {
	gameID := snapshot.GameID
	coreID := snapshot.CoreID
	artifactID := snapshot.CoreArtifactID
	contentID := newID()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_content_revisions(id,
game_id,
content_kind,
source_kind,
source_ref_id,
source_manifest_json,
source_manifest_digest,
created_at_ms) VALUES(?,
?,
?,
'ADMIN_REPLACE',
?,
?,
?,
?)
	`,
		contentID, gameID, prepared.contentKind, jobID,
		string(prepared.manifest), prepared.manifestDigest, now,
	); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := persistReplacementFiles(ctx, transaction, contentID, prepared.files); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	variantID, err := ensureReplacementVariant(ctx, transaction, gameID, coreID, now)
	if err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	var emulatorGameID int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(emulator_game_id),
1000)+1
FROM game_variant_revisions
`).Scan(&emulatorGameID); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	revisionID := newID()
	validationInputDigest, err := replacementValidationDigest(
		snapshot, prepared, biosSnapshot, variantID, contentID, currentDAT,
	)
	if err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_variant_revisions(id,
game_variant_id,
game_content_revision_id,
core_artifact_id,
dat_version_id,
validation_input_digest,
emulator_game_id,
status,
compatibility_code,
dependency_snapshot_json,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
'READY',
'READY',
?,
?)
`,
		revisionID,
		variantID,
		contentID,
		artifactID,
		nullableValue(snapshot.DATVersionID),
		validationInputDigest,
		emulatorGameID,
		string(dependencySnapshotJSON),
		now,
	); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := service.attachReplacementPlaylist(ctx, transaction, revisionID, prepared, now); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	service.finishReplacement(
		ctx, transaction, jobID, snapshot, contentID, variantID, revisionID, now, failTransaction,
	)
}

func (service *Service) finishReplacement(
	ctx context.Context,
	transaction *sql.Tx,
	jobID string,
	snapshot jobSnapshot,
	contentID, variantID, revisionID string,
	now int64,
	failTransaction func(string),
) {
	gameID := snapshot.GameID
	previousContentID := snapshot.BaseContentRevisionID
	if _, err := transaction.ExecContext(ctx, `
UPDATE game_variants
SET current_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, revisionID, now, variantID); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	gameResult, err := transaction.ExecContext(
		ctx,
		`
UPDATE games
SET current_content_revision_id=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND current_content_revision_id=?
AND version=?
`,
		contentID,
		now,
		gameID,
		previousContentID,
		snapshot.GameVersion,
	)
	if err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if changed, _ := gameResult.RowsAffected(); changed != 1 {
		failTransaction("GAME_CONTENT_SNAPSHOT_STALE")
		return
	}
	if service.payloadReleases == nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	impact, err := service.payloadReleases.RetireSupersededGameContent(
		ctx, transaction, gameID, contentID, now,
	)
	if err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
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
		fmt.Sprintf(
			`{"contentRevisionId":%q,"variantRevisionId":%q,"retiredSaveStateCount":%d}`,
			contentID, revisionID, impact.SaveStateCount,
		),
		finished,
	); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	var consumptionID string
	if err := transaction.QueryRowContext(ctx, `
SELECT id FROM upload_consumptions WHERE consumer_type='GAME_FILE_REVISION_JOB' AND consumer_id=?
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
	service.payloadReleases.Signal()
}

func persistReplacementFiles(
	ctx context.Context,
	transaction *sql.Tx,
	contentID string,
	files []replacementFile,
) error {
	for _, value := range files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_content_files(game_content_revision_id,
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

func ensureReplacementVariant(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, coreID string,
	now int64,
) (string, error) {
	var variantID string
	err := transaction.QueryRowContext(ctx, `
SELECT id
FROM game_variants
WHERE game_id=?
AND core_id=?
`, gameID, coreID).
		Scan(&variantID)
	if errors.Is(err, sql.ErrNoRows) {
		variantID = newID()
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_variants(id,
game_id,
core_id,
current_revision_id,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
NULL,
1,
?,
?)
`, variantID, gameID, coreID, now, now); err != nil {
			return "", fmt.Errorf("create replacement variant: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("find replacement variant: %w", err)
	}
	return variantID, nil
}

func (service *Service) attachReplacementPlaylist(
	ctx context.Context,
	transaction *sql.Tx,
	revisionID string,
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
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,'MULTI_DISC_PLAYLIST','playlist.m3u',?,0)
`, revisionID, playlistBlobID); err != nil {
		return fmt.Errorf("attach replacement playlist: %w", err)
	}
	return nil
}

func replacementValidationDigest(
	snapshot jobSnapshot,
	prepared preparedReplacement,
	biosSnapshot corevalidation.Snapshot,
	variantID, contentID string,
	currentDAT sql.NullString,
) (string, error) {
	if prepared.contentKind != multidisc.ContentKind {
		digest, err := corevalidation.ValidationInputDigest(
			snapshot.CoreArtifactID, contentID, currentDAT, biosSnapshot,
		)
		if err != nil {
			return "", fmt.Errorf("digest replacement validation input: %w", err)
		}
		return digest, nil
	}
	biosDigest, err := corevalidation.BIOSDependencyDigest(biosSnapshot)
	if err != nil {
		return "", fmt.Errorf("digest replacement BIOS dependencies: %w", err)
	}
	digest, err := corevalidation.MultiDiscValidationInputDigest(corevalidation.MultiDiscValidationInput{
		GameVariantID: variantID, GameContentRevisionID: contentID,
		ContentKind: prepared.contentKind, CoreArtifactID: snapshot.CoreArtifactID,
		CoreArtifactVersion:       snapshot.CoreArtifactVersion,
		CompatibilityConfigSHA256: snapshot.CompatibilityConfigDigest,
		DATVersionID:              currentDAT, BIOSDependencySHA256: biosDigest,
		OrderedDiscSHA256:       prepared.orderedDiscSHA256,
		CanonicalPlaylistSHA256: prepared.canonicalPlaylist.SHA256,
	})
	if err != nil {
		return "", fmt.Errorf("digest multi-disc replacement validation input: %w", err)
	}
	return digest, nil
}
