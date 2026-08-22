package metadatascrape

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
)

func nullableText(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) ScheduleReview(
	ctx context.Context,
	itemID string,
	expectedVersion int64,
	providerName string,
) (Scheduled, int64, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var currentVersion int64
	var before string
	if err := transaction.QueryRowContext(ctx, `
SELECT d.version,
d.metadata_json
FROM review_drafts d
JOIN import_items i ON i.id=d.import_item_id
WHERE i.id=?
AND i.state='REVIEW_PENDING'
`, itemID).Scan(&currentVersion, &before); err != nil ||
		currentVersion != expectedVersion {
		return Scheduled{}, 0, errReviewVersionConflict
	}
	nonce := newID()
	scheduled, err := service.schedule(
		ctx,
		transaction,
		itemID,
		providerName,
		"metadata-review-v1:"+itemID+":"+nonce,
		true,
	)
	if err != nil {
		return Scheduled{}, 0, err
	}
	now := service.now().UnixMilli()
	result, err := transaction.ExecContext(
		ctx,
		`
UPDATE review_drafts
SET version=version+1,
updated_at_ms=?
WHERE import_item_id=?
AND version=?
`,
		now,
		itemID,
		expectedVersion,
	)
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return Scheduled{}, 0, errReviewVersionConflict
	}
	after, _ := json.Marshal(
		map[string]any{"metadataProvider": providerName, "scrapeRunId": scheduled.RunID, "jobId": scheduled.JobID},
	)
	actor := authn.ActorFromContext(ctx, "release-setup")
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,
import_item_id,
event_type,
actor_kind,
actor_user_id,
actor_label,
before_json,
after_json,
diff_json,
config_evidence_json,
dat_evidence_json,
provider_evidence_json,
created_at_ms) VALUES(?,
?,
'SCRAPE_REQUESTED',
?,
?,
?,
?,
?,
?,
'{}',
'{}',
? ,
?)
`,
		newID(), itemID, actor.Kind, actor.UserID, actor.Label,
		before, string(after), string(after), string(after), now,
	); err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if !scheduled.Noop {
		go func() { _ = service.Run(context.WithoutCancel(ctx), scheduled.RunID) }()
	}
	return scheduled, expectedVersion + 1, nil
}

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) ScheduleGame(
	ctx context.Context,
	gameID string,
	expectedVersion int64,
) (Scheduled, int64, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var contentID, platformID string
	var currentVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT g.current_content_revision_id,
g.version,
p.platform_id
FROM games g
JOIN platform_instances p ON p.id=g.platform_instance_id
WHERE g.id=?
AND g.status='PUBLISHED'
`, gameID).Scan(&contentID, &currentVersion, &platformID); err != nil ||
		currentVersion != expectedVersion {
		return Scheduled{}, 0, errGameVersionConflict
	}
	runID, jobID := newID(), newID()
	now := service.now().UnixMilli()
	dedupe := sha256.Sum256([]byte("metadata-game-v1:" + gameID + ":" + contentID + ":" + runID))
	payload, _ := json.Marshal(
		map[string]any{"contentRevisionId": contentID, "gameId": gameID, "provider": "HASHEOUS", "bypassCache": true},
	)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'GAME',
?,
'METADATA_SCRAPE',
?,
1,
?,
1,
'QUEUED',
0,
4,
?,
?,
?)
`, jobID, gameID, hex.EncodeToString(dedupe[:]), string(payload), now, now, now); err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO metadata_scrape_runs(id,
game_id,
game_content_revision_id,
job_id,
provider,
provider_config_version,
state,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
'HASHEOUS',
1,
'RUNNING',
?,
?)
`, runID, gameID, contentID, jobID, now, now); err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if err := service.scheduleGameEvidence(
		ctx, transaction, gameID, contentID, platformID, runID, now,
	); err != nil {
		return Scheduled{}, 0, err
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
'QUEUED',
'{}',
?)
`, jobID, gameID, now); err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	result, err := transaction.ExecContext(
		ctx,
		`
UPDATE games
SET version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
AND current_content_revision_id=?
`,
		now,
		gameID,
		expectedVersion,
		contentID,
	)
	if err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Scheduled{}, 0, errGameVersionConflict
	}
	if err := transaction.Commit(); err != nil {
		return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
	}
	go func() { _ = service.Run(context.WithoutCancel(ctx), runID) }()
	return Scheduled{RunID: runID, JobID: jobID}, expectedVersion + 1, nil
}

func (service *Service) scheduleGameEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, contentID, platformID, runID string,
	now int64,
) error {
	if platformID == "arcade" {
		return service.scheduleGameArcadeEvidence(ctx, transaction, gameID, contentID, runID, now)
	}
	return service.scheduleGameContentEvidence(ctx, transaction, contentID, runID, now)
}

func (service *Service) scheduleGameContentEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	contentID, runID string,
	now int64,
) error {
	rows, err := transaction.QueryContext(ctx, `
SELECT f.logical_name,b.id,b.crc32,b.md5,b.sha1,b.sha256,
f.source_archive_blob_id,f.source_archive_entry_ordinal
FROM game_content_files f JOIN blobs b ON b.id=f.blob_id
WHERE f.game_content_revision_id=? AND f.role='CONTENT'
ORDER BY f.sort_order,f.logical_name
`, contentID)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	order := 0
	for rows.Next() {
		var path, blobID, crc32Value, md5Value, sha1Value, sha256Value string
		var archiveBlobID sql.NullString
		var archiveOrdinal sql.NullInt64
		if err := rows.Scan(
			&path, &blobID, &crc32Value, &md5Value, &sha1Value, &sha256Value,
			&archiveBlobID, &archiveOrdinal,
		); err != nil {
			return fmt.Errorf("metadatascrape/service: %w", err)
		}
		if strings.EqualFold(filepath.Ext(path), ".zip") && !archiveBlobID.Valid {
			continue
		}
		if err := insertGameContentEvidence(
			ctx, transaction, runID, blobID, archiveBlobID, archiveOrdinal,
			crc32Value, md5Value, sha1Value, sha256Value, order, now,
		); err != nil {
			return err
		}
		order++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	return nil
}

func insertGameContentEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	runID, blobID string,
	archiveBlobID sql.NullString,
	archiveOrdinal sql.NullInt64,
	crc32Value, md5Value, sha1Value, sha256Value string,
	order int,
	now int64,
) error {
	if archiveBlobID.Valid && archiveOrdinal.Valid {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO content_hash_evidence(id,scrape_run_id,profile,blob_id,archive_blob_id,
archive_entry_ordinal,crc32,md5,sha1,sha256,query_order,created_at_ms)
VALUES(?,?,'SINGLE_ARCHIVE_MEMBER_V1',NULL,?,?,?,?,?,?,?,?)
`, newID(), runID, archiveBlobID.String, archiveOrdinal.Int64,
			crc32Value, md5Value, sha1Value, sha256Value, order, now)
		if err != nil {
			return fmt.Errorf("metadatascrape/service: %w", err)
		}
		return nil
	}
	_, err := transaction.ExecContext(ctx, `
INSERT INTO content_hash_evidence(id,scrape_run_id,profile,blob_id,crc32,md5,sha1,sha256,
query_order,created_at_ms) VALUES(?,?,'RAW_FILE_V1',?,?,?,?,?,?,?)
`, newID(), runID, blobID, crc32Value, md5Value, sha1Value, sha256Value, order, now)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	return nil
}
