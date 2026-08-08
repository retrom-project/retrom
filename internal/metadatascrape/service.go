package metadatascrape

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/hasheous"
)

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	provider *hasheous.Provider
	now      func() time.Time
}

var (
	errProviderInvalid       = errors.New("METADATA_PROVIDER_INVALID")
	errArcadeSnapshotInvalid = errors.New("ARCADE_EVIDENCE_SNAPSHOT_INVALID")
	errReviewVersionConflict = errors.New("REVIEW_VERSION_CONFLICT")
	errGameVersionConflict   = errors.New("GAME_VERSION_CONFLICT")
	errAssetStateConflict    = errors.New("ASSET_STATE_CONFLICT")
	errInitialItemState      = errors.New("initial review item state changed")
	errInitialProgressState  = errors.New("initial import progress state changed")
)

type Scheduled struct {
	RunID string
	JobID string
	Noop  bool
}

func (scheduled Scheduled) ScrapeRunID() string { return scheduled.RunID }

func (scheduled Scheduled) IsNoop() bool { return scheduled.Noop }

func New(database *sql.DB, blobs *blobstore.Store, provider *hasheous.Provider, now func() time.Time) *Service {
	return &Service{database: database, blobs: blobs, provider: provider, now: now}
}

func (service *Service) ScheduleImport(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, providerName string,
) (Scheduled, error) {
	return service.schedule(
		ctx,
		transaction,
		itemID,
		providerName,
		"metadata-import-v1:"+itemID+":"+providerName,
		false,
	)
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) schedule(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, providerName, dedupeInput string,
	bypassCache bool,
) (Scheduled, error) {
	if providerName != "HASHEOUS" && providerName != "NONE" {
		return Scheduled{}, errProviderInvalid
	}
	now := service.now().UnixMilli()
	runID, jobID := newID(), newID()
	dedupe := sha256.Sum256([]byte(dedupeInput))
	jobState, runState := "QUEUED", "RUNNING"
	var finished any
	if providerName == "NONE" {
		jobState, runState, finished = "SUCCEEDED", "COMPLETED", now
	}
	payload, _ := json.Marshal(map[string]any{"provider": providerName, "bypassCache": bypassCache})
	_, err := transaction.ExecContext(
		ctx,
		`
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
finished_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
'METADATA_SCRAPE',
?,
1,
?,
1,
?,
0,
4,
?,
?,
?,
?)
`,
		jobID,
		itemID,
		hex.EncodeToString(dedupe[:]),
		string(payload),
		jobState,
		now,
		finished,
		now,
		now,
	)
	if err != nil {
		return Scheduled{}, fmt.Errorf("create metadata job: %w", err)
	}
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO metadata_scrape_runs(id,
import_item_id,
job_id,
provider,
provider_config_version,
state,
created_at_ms,
updated_at_ms,
completed_at_ms) VALUES(?,
?,
?,
 ?,
1,
?,
?,
?,
?)
`,
		runID,
		itemID,
		jobID,
		providerName,
		runState,
		now,
		now,
		finished,
	)
	if err != nil {
		return Scheduled{}, fmt.Errorf("create metadata run: %w", err)
	}
	event := "QUEUED"
	if providerName == "NONE" {
		event = "SUCCEEDED"
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'IMPORT_ITEM',
?,
?,
?,
?)
`, jobID, itemID, event, fmt.Sprintf(`{"provider":%q}`, providerName), now); err != nil {
		return Scheduled{}, fmt.Errorf("create metadata event: %w", err)
	}
	if providerName == "NONE" {
		return Scheduled{RunID: runID, JobID: jobID, Noop: true}, nil
	}
	var platformID string
	if err := transaction.QueryRowContext(ctx, `
SELECT j.platform_id
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
WHERE i.id=?
`, itemID).Scan(&platformID); err != nil {
		return Scheduled{}, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if platformID == "arcade" {
		if err := service.scheduleImportArcadeEvidence(ctx, transaction, itemID, runID, now); err != nil {
			return Scheduled{}, err
		}
		return Scheduled{RunID: runID, JobID: jobID}, nil
	}
	rows, err := transaction.QueryContext(
		ctx,
		`
SELECT s.logical_name,
b.id,
b.crc32,
b.md5,
b.sha1,
b.sha256,
s.source_archive_blob_id,
s.source_archive_entry_ordinal
FROM import_item_source_files s
JOIN blobs b ON b.id=s.blob_id
WHERE s.import_item_id=?
AND s.role='CONTENT'
ORDER BY s.sort_order,
s.logical_name
`,
		itemID,
	)
	if err != nil {
		return Scheduled{}, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	order := 0
	for rows.Next() {
		var path, blobID, crc32Value, md5Value, sha1Value, sha256Value string
		var archiveBlobID sql.NullString
		var archiveOrdinal sql.NullInt64
		if err := rows.Scan(
			&path,
			&blobID,
			&crc32Value,
			&md5Value,
			&sha1Value,
			&sha256Value,
			&archiveBlobID,
			&archiveOrdinal,
		); err != nil {
			return Scheduled{}, fmt.Errorf("metadatascrape/service: %w", err)
		}
		if strings.EqualFold(filepath.Ext(path), ".zip") && !archiveBlobID.Valid {
			continue
		}
		evidenceID := newID()
		if archiveBlobID.Valid && archiveOrdinal.Valid {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO content_hash_evidence(id,
scrape_run_id,
profile,
blob_id,
archive_blob_id,
archive_entry_ordinal,
crc32,
md5,
sha1,
sha256,
query_order,
created_at_ms) VALUES(?,
?,
'SINGLE_ARCHIVE_MEMBER_V1',
NULL,
?,
?,
?,
?,
?,
?,
?,
?)
`,
				evidenceID,
				runID,
				archiveBlobID.String,
				archiveOrdinal.Int64,
				crc32Value,
				md5Value,
				sha1Value,
				sha256Value,
				order,
				now,
			); err != nil {
				return Scheduled{}, fmt.Errorf("metadatascrape/service: %w", err)
			}
		} else {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO content_hash_evidence(id,
scrape_run_id,
profile,
blob_id,
crc32,
md5,
sha1,
sha256,
query_order,
created_at_ms) VALUES(?,
?,
'RAW_FILE_V1',
?,
?,
?,
?,
?,
?,
?)
`, evidenceID, runID, blobID, crc32Value, md5Value, sha1Value, sha256Value, order, now); err != nil {
				return Scheduled{}, fmt.Errorf("metadatascrape/service: %w", err)
			}
		}
		order++
	}
	if err := rows.Err(); err != nil {
		return Scheduled{}, fmt.Errorf("metadatascrape/service: %w", err)
	}
	return Scheduled{RunID: runID, JobID: jobID}, nil
}

type arcadeEvidence struct {
	archiveBlobID string
	ordinal       int64
	name          string
	size          int64
	crc32         sql.NullString
	sha1          sql.NullString
}

func (service *Service) scheduleImportArcadeEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, runID string,
	now int64,
) error {
	var datID, dependencySnapshot string
	err := transaction.QueryRowContext(ctx, `
SELECT v.dat_version_id,
v.dependency_snapshot_json
FROM import_item_core_validations v
WHERE v.import_item_id=?
AND v.dat_version_id IS NOT NULL
ORDER BY v.created_at_ms DESC,
v.id DESC LIMIT 1
`, itemID).Scan(&datID, &dependencySnapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	var snapshot struct {
		Machine string `json:"machine"`
	}
	if err := json.Unmarshal([]byte(dependencySnapshot), &snapshot); err != nil || snapshot.Machine == "" {
		return errArcadeSnapshotInvalid
	}
	rows, err := transaction.QueryContext(ctx, `
SELECT s.blob_id,
e.ordinal,
d.name,
d.size_bytes,
d.crc32,
d.sha1
FROM import_item_source_files s
JOIN archive_entries e ON e.archive_blob_id=s.blob_id
JOIN dat_rom_entries d ON d.dat_version_id=?
AND d.machine_name=?
AND d.name=e.normalized_path
WHERE s.import_item_id=?
AND s.role='CONTENT'
AND COALESCE(d.status,
'GOOD')!='NODUMP'
AND (d.bios_name IS NULL
OR d.bios_name=(SELECT bios_name
FROM dat_bios_sets
WHERE dat_version_id=d.dat_version_id
AND machine_name=d.machine_name
AND is_default=1))
AND (d.crc32 IS NOT NULL
OR d.sha1 IS NOT NULL)
AND e.uncompressed_size_bytes=d.size_bytes
AND (d.crc32 IS NULL
OR lower(e.crc32)=lower(d.crc32))
AND (d.sha1 IS NULL
OR lower(e.sha1)=lower(d.sha1))
`, datID, snapshot.Machine, itemID)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	return service.insertArcadeEvidence(ctx, transaction, runID, now, rows)
}

func (service *Service) scheduleGameArcadeEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, contentID, runID string,
	now int64,
) error {
	var datID, dependencySnapshot string
	err := transaction.QueryRowContext(ctx, `
SELECT r.dat_version_id,
r.dependency_snapshot_json
FROM games g
JOIN platform_instances p ON p.id=g.platform_instance_id
JOIN game_variants v ON v.game_id=g.id
AND v.core_id=p.default_core_id
JOIN game_variant_revisions r ON r.id=v.current_revision_id
AND r.game_content_revision_id=?
WHERE g.id=?
AND r.dat_version_id IS NOT NULL
`, contentID, gameID).Scan(&datID, &dependencySnapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	var snapshot struct {
		Machine string `json:"machine"`
	}
	if err := json.Unmarshal([]byte(dependencySnapshot), &snapshot); err != nil || snapshot.Machine == "" {
		return errArcadeSnapshotInvalid
	}
	rows, err := transaction.QueryContext(ctx, `
SELECT f.blob_id,
e.ordinal,
d.name,
d.size_bytes,
d.crc32,
d.sha1
FROM game_content_files f
JOIN archive_entries e ON e.archive_blob_id=f.blob_id
JOIN dat_rom_entries d ON d.dat_version_id=?
AND d.machine_name=?
AND d.name=e.normalized_path
WHERE f.game_content_revision_id=?
AND f.role='CONTENT'
AND COALESCE(d.status,
'GOOD')!='NODUMP'
AND (d.bios_name IS NULL
OR d.bios_name=(SELECT bios_name
FROM dat_bios_sets
WHERE dat_version_id=d.dat_version_id
AND machine_name=d.machine_name
AND is_default=1))
AND (d.crc32 IS NOT NULL
OR d.sha1 IS NOT NULL)
AND e.uncompressed_size_bytes=d.size_bytes
AND (d.crc32 IS NULL
OR lower(e.crc32)=lower(d.crc32))
AND (d.sha1 IS NULL
OR lower(e.sha1)=lower(d.sha1))
`, datID, snapshot.Machine, contentID)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	return service.insertArcadeEvidence(ctx, transaction, runID, now, rows)
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (service *Service) insertArcadeEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	now int64,
	rows *sql.Rows,
) error {
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]arcadeEvidence, 0)
	for rows.Next() {
		var entry arcadeEvidence
		if err := rows.Scan(
			&entry.archiveBlobID,
			&entry.ordinal,
			&entry.name,
			&entry.size,
			&entry.crc32,
			&entry.sha1,
		); err != nil {
			return fmt.Errorf("metadatascrape/service: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].sha1.Valid != entries[right].sha1.Valid {
			return entries[left].sha1.Valid
		}
		if entries[left].size != entries[right].size {
			return entries[left].size > entries[right].size
		}
		return entries[left].name < entries[right].name
	})
	seen := make(map[string]struct{}, len(entries))
	order := 0
	for _, entry := range entries {
		key := entry.crc32.String + "\x00" + entry.sha1.String
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO content_hash_evidence(id,
scrape_run_id,
profile,
blob_id,
archive_blob_id,
archive_entry_ordinal,
crc32,
md5,
sha1,
sha256,
query_order,
created_at_ms) VALUES(?,
?,
'ARCADE_DAT_ENTRIES_V1',
NULL,
?,
?,
?,
NULL,
?,
NULL,
?,
?)
`,
			newID(),
			runID,
			entry.archiveBlobID,
			entry.ordinal,
			nullableText(entry.crc32),
			nullableText(entry.sha1),
			order,
			now,
		); err != nil {
			return fmt.Errorf("metadatascrape/service: %w", err)
		}
		order++
		if order == 8 {
			break
		}
	}
	return nil
}

func nullableText(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
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
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,
import_item_id,
event_type,
actor,
before_json,
after_json,
diff_json,
config_evidence_json,
dat_evidence_json,
provider_evidence_json,
created_at_ms) VALUES(?,
?,
'SCRAPE_REQUESTED',
'local',
?,
?,
?,
'{}',
'{}',
? ,
?)
`, newID(), itemID, before, string(after), string(after), string(after), now); err != nil {
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

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
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
	if platformID == "arcade" {
		if err := service.scheduleGameArcadeEvidence(ctx, transaction, gameID, contentID, runID, now); err != nil {
			return Scheduled{}, 0, err
		}
	} else {
		rows, err := transaction.QueryContext(ctx, `
SELECT f.logical_name,
b.id,
b.crc32,
b.md5,
b.sha1,
b.sha256,
f.source_archive_blob_id,
f.source_archive_entry_ordinal
FROM game_content_files f
JOIN blobs b ON b.id=f.blob_id
WHERE f.game_content_revision_id=?
AND f.role='CONTENT'
ORDER BY f.sort_order,
f.logical_name
`, contentID)
		if err != nil {
			return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
		}
		defer func() { cleanup.Error("close", rows.Close()) }()
		order := 0
		for rows.Next() {
			var path, blobID, crc32Value, md5Value, sha1Value, sha256Value string
			var archiveBlobID sql.NullString
			var archiveOrdinal sql.NullInt64
			if err := rows.Scan(
				&path,
				&blobID,
				&crc32Value,
				&md5Value,
				&sha1Value,
				&sha256Value,
				&archiveBlobID,
				&archiveOrdinal,
			); err != nil {
				return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
			}
			if strings.EqualFold(filepath.Ext(path), ".zip") && !archiveBlobID.Valid {
				continue
			}
			if archiveBlobID.Valid && archiveOrdinal.Valid {
				if _, err := transaction.ExecContext(ctx, `
INSERT INTO content_hash_evidence(id,
scrape_run_id,
profile,
blob_id,
archive_blob_id,
archive_entry_ordinal,
crc32,
md5,
sha1,
sha256,
query_order,
created_at_ms) VALUES(?,
?,
'SINGLE_ARCHIVE_MEMBER_V1',
NULL,
?,
?,
?,
?,
?,
?,
?,
?)
`,
					newID(),
					runID,
					archiveBlobID.String,
					archiveOrdinal.Int64,
					crc32Value,
					md5Value,
					sha1Value,
					sha256Value,
					order,
					now,
				); err != nil {
					return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
				}
			} else {
				if _, err := transaction.ExecContext(ctx, `
INSERT INTO content_hash_evidence(id,
scrape_run_id,
profile,
blob_id,
crc32,
md5,
sha1,
sha256,
query_order,
created_at_ms) VALUES(?,
?,
'RAW_FILE_V1',
?,
?,
?,
?,
?,
?,
?)
`, newID(), runID, blobID, crc32Value, md5Value, sha1Value, sha256Value, order, now); err != nil {
					return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
				}
			}
			order++
		}
		if err := rows.Err(); err != nil {
			return Scheduled{}, 0, fmt.Errorf("metadatascrape/service: %w", err)
		}
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

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Run(ctx context.Context, runID string) error {
	var jobID, providerName, state, payloadJSON string
	if err := service.database.QueryRowContext(ctx, `
SELECT r.job_id,
r.provider,
r.state,
j.payload_json
FROM metadata_scrape_runs r
JOIN jobs j ON j.id=r.job_id
WHERE r.id=?
`, runID).Scan(&jobID, &providerName, &state, &payloadJSON); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if providerName == "NONE" || state != "RUNNING" {
		return nil
	}
	var payload struct {
		BypassCache bool `json:"bypassCache"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_JOB_PAYLOAD_INVALID", err)
	}
	now := service.now().UnixMilli()
	if _, err := service.database.ExecContext(ctx, `
UPDATE jobs
SET state='RUNNING',
attempt_count=attempt_count+1,
execution_started_at_ms=?,
execution_deadline_at_ms=?,
leased_until_ms=?,
heartbeat_at_ms=?,
worker_id='local',
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`, now, now+15_000, now+60_000, now, now, jobID); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_JOB_START_FAILED", err)
	}
	if _, err := service.database.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'STARTED',
'{}',
?
FROM jobs
WHERE id=?
`, now, jobID); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_EVENT_FAILED", err)
	}
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT id,
crc32,
md5,
sha1,
sha256
FROM content_hash_evidence
WHERE scrape_run_id=?
ORDER BY query_order,
id LIMIT 8
`,
		runID,
	)
	if err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_EVIDENCE_FAILED", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type evidence struct {
		id                       string
		crc32, md5, sha1, sha256 sql.NullString
	}
	evidenceList := make([]evidence, 0, 8)
	for rows.Next() {
		var value evidence
		if err := rows.Scan(&value.id, &value.crc32, &value.md5, &value.sha1, &value.sha256); err != nil {
			return service.fail(ctx, runID, jobID, "METADATA_EVIDENCE_FAILED", err)
		}
		evidenceList = append(evidenceList, value)
	}
	if err := rows.Err(); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_EVIDENCE_FAILED", err)
	}
	candidateCount := 0
	for _, item := range evidenceList {
		hashes := hasheous.ContentHashes{
			CRC32:  item.crc32.String,
			MD5:    item.md5.String,
			SHA1:   item.sha1.String,
			SHA256: item.sha256.String,
		}
		for attempt := 1; attempt <= 3; attempt++ {
			resolved, lookupErr := service.lookup(ctx, hashes, payload.BypassCache)
			if lookupErr != nil {
				return service.fail(ctx, runID, jobID, "METADATA_REQUEST_INVALID", lookupErr)
			}
			created, persistErr := service.persistResult(
				ctx,
				runID,
				item.id,
				resolved.result,
				resolved.cachedResponseID,
				attempt,
				candidateCount < 20,
			)
			if persistErr != nil {
				return service.fail(ctx, runID, jobID, "METADATA_PERSIST_FAILED", persistErr)
			}
			if created {
				candidateCount++
			}
			if resolved.cachedResponseID != "" || !retryableOutcome(resolved.result.Outcome) || attempt == 3 {
				break
			}
			if err := waitRetry(ctx, time.Duration(100*(1<<(attempt-1)))*time.Millisecond); err != nil {
				return service.fail(ctx, runID, jobID, "METADATA_CANCELLED", err)
			}
		}
	}
	if err := service.fetchPendingAssets(ctx, runID); err != nil {
		return service.fail(ctx, runID, jobID, "METADATA_ASSET_PERSIST_FAILED", err)
	}
	return service.complete(ctx, runID, jobID, candidateCount)
}

type resolvedLookup struct {
	result           hasheous.LookupResult
	cachedResponseID string
}

//nolint:gocognit,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) lookup(
	ctx context.Context,
	hashes hasheous.ContentHashes,
	bypassCache bool,
) (resolvedLookup, error) {
	digest, err := hasheous.RequestDigest(hashes)
	if err != nil {
		return resolvedLookup{}, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if !bypassCache {
		var responseID, outcome string
		var status sql.NullInt64
		var rawSHA sql.NullString
		err := service.database.QueryRowContext(ctx, `
SELECT r.id,
r.outcome,
r.http_status,
b.sha256
FROM metadata_provider_cache c
JOIN metadata_provider_responses r ON r.id=c.current_response_id
LEFT JOIN blobs b ON b.id=r.raw_response_blob_id
WHERE c.provider='HASHEOUS'
AND c.request_digest=?
AND c.expires_at_ms>?
`, digest, service.now().UnixMilli()).
			Scan(&responseID, &outcome, &status, &rawSHA)
		if err == nil {
			raw := []byte(nil)
			if rawSHA.Valid {
				file, openErr := service.blobs.OpenDigest(rawSHA.String)
				if openErr == nil {
					raw, openErr = io.ReadAll(io.LimitReader(file, (4<<20)+1))
					cleanup.Error("close", file.Close())
				}
				if openErr != nil || len(raw) > 4<<20 {
					raw = nil
				}
			}
			if outcome == string(hasheous.OutcomeMiss) || len(raw) > 0 {
				restored, restoreErr := service.provider.RestoreCached(
					hashes,
					hasheous.ProviderOutcome(outcome),
					int(status.Int64),
					raw,
				)
				if restoreErr == nil {
					return resolvedLookup{result: restored, cachedResponseID: responseID}, nil
				}
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return resolvedLookup{}, fmt.Errorf("metadatascrape/service: %w", err)
		}
	}
	result, err := service.provider.LookupByHash(ctx, hashes)
	if err != nil {
		return resolvedLookup{}, fmt.Errorf("look up metadata by hash: %w", err)
	}
	return resolvedLookup{result: result}, nil
}

func retryableOutcome(outcome hasheous.ProviderOutcome) bool {
	return outcome == hasheous.OutcomeRateLimited || outcome == hasheous.OutcomeTimeout ||
		outcome == hasheous.OutcomeNetworkError
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("metadatascrape/service: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) persistResult(
	ctx context.Context,
	runID, evidenceID string,
	result hasheous.LookupResult,
	cachedResponseID string,
	attemptNo int,
	allowCandidate bool,
) (bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	responseID, attemptID := cachedResponseID, newID()
	source := "CACHE"
	if responseID == "" {
		source = "NETWORK"
		var rawBlobID any
		if len(result.RawResponse) > 0 {
			metadata, putErr := service.blobs.Put(bytes.NewReader(result.RawResponse))
			if putErr != nil {
				return false, fmt.Errorf("metadatascrape/service: %w", putErr)
			}
			blobID, registerErr := blobstore.EnsureRecord(ctx, transaction, metadata, "application/json", now)
			if registerErr != nil {
				return false, fmt.Errorf("metadatascrape/service: %w", registerErr)
			}
			rawBlobID = blobID
		}
		responseID = newID()
		expiresAt := now
		switch result.Outcome {
		case hasheous.OutcomeHit:
			expiresAt = service.now().Add(7 * 24 * time.Hour).UnixMilli()
		case hasheous.OutcomeMiss:
			expiresAt = service.now().Add(24 * time.Hour).UnixMilli()
		case hasheous.OutcomeRateLimited,
			hasheous.OutcomeTimeout,
			hasheous.OutcomeInvalidResponse,
			hasheous.OutcomeNetworkError:
			// Transient and invalid responses are evidence only and expire immediately.
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO metadata_provider_responses(id,
provider,
request_digest,
http_status,
outcome,
raw_response_blob_id,
fetched_at_ms,
expires_at_ms) VALUES(?,
'HASHEOUS',
?,
?,
?,
?,
?,
?)
`,
			responseID,
			result.RequestDigest,
			nullableStatus(result.HTTPStatus),
			string(result.Outcome),
			rawBlobID,
			now,
			expiresAt,
		); err != nil {
			return false, fmt.Errorf("metadatascrape/service: %w", err)
		}
		if result.Outcome == hasheous.OutcomeHit || result.Outcome == hasheous.OutcomeMiss {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO metadata_provider_cache(provider,
request_digest,
current_response_id,
expires_at_ms,
updated_at_ms) VALUES('HASHEOUS',
?,
?,
?,
?) ON CONFLICT(provider,
request_digest)
DO UPDATE SET current_response_id=excluded.current_response_id,
expires_at_ms=excluded.expires_at_ms,
updated_at_ms=excluded.updated_at_ms
`, result.RequestDigest, responseID, expiresAt, now); err != nil {
				return false, fmt.Errorf("metadatascrape/service: %w", err)
			}
		}
	}
	if source == "CACHE" {
		attemptNo = 1
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO metadata_scrape_query_attempts(id,
scrape_run_id,
content_hash_evidence_id,
provider_response_id,
attempt_no,
source,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?)
`, attemptID, runID, evidenceID, responseID, attemptNo, source, now); err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if result.Candidate == nil || !allowCandidate {
		if err := transaction.Commit(); err != nil {
			return false, fmt.Errorf("commit metadata lookup without candidate: %w", err)
		}
		return false, nil
	}
	metadataJSON, _ := json.Marshal(result.Candidate.Metadata)
	evidenceJSON, _ := json.Marshal(result.Candidate.Evidence)
	candidateID := newID()
	resultInsert, err := transaction.ExecContext(
		ctx,
		`
INSERT INTO scrape_candidates(id,
scrape_run_id,
primary_response_id,
provider_game_id,
normalized_metadata_json,
evidence_json,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?) ON CONFLICT(scrape_run_id,
provider_game_id) DO NOTHING
`,
		candidateID,
		runID,
		responseID,
		result.Candidate.ProviderGameID,
		string(metadataJSON),
		string(evidenceJSON),
		now,
	)
	if err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	inserted, _ := resultInsert.RowsAffected()
	if inserted == 0 {
		if err := transaction.QueryRowContext(ctx, `
SELECT id
FROM scrape_candidates
WHERE scrape_run_id=?
AND provider_game_id=?
`, runID, result.Candidate.ProviderGameID).Scan(&candidateID); err != nil {
			return false, fmt.Errorf("metadatascrape/service: %w", err)
		}
	}
	var crc32Value, md5Value, sha1Value, sha256Value sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT crc32,
md5,
sha1,
sha256
FROM content_hash_evidence
WHERE id=?
`, evidenceID).Scan(&crc32Value, &md5Value, &sha1Value, &sha256Value); err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	matched := make(map[string]string, 4)
	hashes := map[string]sql.NullString{
		"crc32": crc32Value, "md5": md5Value, "sha1": sha1Value, "sha256": sha256Value,
	}
	for key, value := range hashes {
		if value.Valid {
			matched[key] = value.String
		}
	}
	matchedJSON, _ := json.Marshal(matched)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO scrape_candidate_hits(scrape_candidate_id,
query_attempt_id,
matched_hashes_json,
created_at_ms) VALUES(?,
?,
?,
?)
`, candidateID, attemptID, string(matchedJSON), now); err != nil {
		return false, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if inserted == 1 {
		for _, asset := range result.Candidate.Assets {
			assetID := newID()
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO scrape_candidate_assets(id,
scrape_candidate_id,
provider_response_id,
provider_asset_id,
kind_hint,
ordinal,
source_path,
status,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
'PENDING',
1,
?,
?)
`,
				assetID,
				candidateID,
				responseID,
				asset.ProviderAssetID,
				asset.Kind,
				asset.Ordinal,
				asset.Path,
				now,
				now,
			); err != nil {
				return false, fmt.Errorf("metadatascrape/service: %w", err)
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit metadata candidate: %w", err)
	}
	return inserted == 1, nil
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) fetchPendingAssets(ctx context.Context, runID string) error {
	rows, err := service.database.QueryContext(ctx, `
SELECT a.id,
a.provider_asset_id,
a.kind_hint,
a.ordinal,
a.source_path
FROM scrape_candidate_assets a
JOIN scrape_candidates c ON c.id=a.scrape_candidate_id
WHERE c.scrape_run_id=?
AND a.status='PENDING'
ORDER BY (SELECT count(*)
FROM scrape_candidate_hits h
WHERE h.scrape_candidate_id=c.id) DESC,
  (SELECT min(e.query_order)
FROM scrape_candidate_hits h
JOIN metadata_scrape_query_attempts q ON q.id=h.query_attempt_id
JOIN content_hash_evidence e ON e.id=q.content_hash_evidence_id
WHERE h.scrape_candidate_id=c.id),
  c.provider_game_id,
a.kind_hint,
a.ordinal,
a.id
`, runID)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	type pendingAsset struct {
		id  string
		ref hasheous.AssetRef
	}
	assets := make([]pendingAsset, 0)
	for rows.Next() {
		var asset pendingAsset
		if err := rows.Scan(
			&asset.id,
			&asset.ref.ProviderAssetID,
			&asset.ref.Kind,
			&asset.ref.Ordinal,
			&asset.ref.Path,
		); err != nil {
			return fmt.Errorf("metadatascrape/service: %w", err)
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	var consumed int64
	for _, asset := range assets {
		if consumed >= 100<<20 {
			if err := service.markAssetFailed(ctx, asset.id, "ASSET_RUN_BUDGET_EXCEEDED"); err != nil {
				return err
			}
			continue
		}
		data, fetchErr := service.provider.FetchAsset(ctx, asset.ref)
		if fetchErr != nil {
			if err := service.markAssetFailed(ctx, asset.id, stableAssetError(fetchErr)); err != nil {
				return err
			}
			continue
		}
		consumed += int64(len(data.Bytes))
		if consumed > 100<<20 {
			if err := service.markAssetFailed(ctx, asset.id, "ASSET_RUN_BUDGET_EXCEEDED"); err != nil {
				return err
			}
			continue
		}
		metadata, putErr := service.blobs.Put(bytes.NewReader(data.Bytes))
		if putErr != nil {
			return fmt.Errorf("metadatascrape/service: %w", putErr)
		}
		transaction, beginErr := service.database.BeginTx(ctx, nil)
		if beginErr != nil {
			return fmt.Errorf("metadatascrape/service: %w", beginErr)
		}
		blobID, registerErr := blobstore.EnsureRecord(
			ctx,
			transaction,
			metadata,
			data.MediaType,
			service.now().UnixMilli(),
		)
		if registerErr == nil {
			_, registerErr = transaction.ExecContext(
				ctx,
				`
UPDATE scrape_candidate_assets
SET status='READY',
blob_id=?,
width_px=?,
height_px=?,
media_type=?,
fetched_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND status='PENDING'
`,
				blobID,
				data.Width,
				data.Height,
				data.MediaType,
				service.now().UnixMilli(),
				service.now().UnixMilli(),
				asset.id,
			)
		}
		if registerErr == nil {
			registerErr = transaction.Commit()
		} else {
			cleanup.Rollback(transaction)
		}
		if registerErr != nil {
			return fmt.Errorf("metadatascrape/service: %w", registerErr)
		}
	}
	return nil
}

func (service *Service) markAssetFailed(ctx context.Context, assetID, code string) error {
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(
		ctx,
		`
UPDATE scrape_candidate_assets
SET status='FAILED',
error_code=?,
fetched_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND status='PENDING'
`,
		code,
		now,
		now,
		assetID,
	)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return errAssetStateConflict
	}
	return nil
}

func stableAssetError(err error) string {
	for _, known := range []error{
		hasheous.ErrAssetURLInvalid,
		hasheous.ErrAssetNetwork,
		hasheous.ErrAssetRedirectLimit,
		hasheous.ErrAssetHTTPStatus,
		hasheous.ErrAssetTooLarge,
		hasheous.ErrAssetURLRejected,
		hasheous.ErrAssetDNSFailed,
		hasheous.ErrAssetIPRejected,
		hasheous.ErrAssetMediaTypeInvalid,
		hasheous.ErrAssetMediaTypeMismatch,
		hasheous.ErrAssetDecodeFailed,
		hasheous.ErrAssetPixelLimit,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "ASSET_FETCH_FAILED"
}

type initialReviewMetadata struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Developer   string `json:"developer"`
	Publisher   string `json:"publisher"`
	Genre       string `json:"genre"`
	Players     *int   `json:"players"`
	ReleaseYear *int   `json:"releaseYear"`
}

func mergeInitialReviewMetadata(currentJSON, candidateJSON string) (string, string, error) {
	var current, candidate initialReviewMetadata
	if err := json.Unmarshal([]byte(currentJSON), &current); err != nil {
		return "", "", fmt.Errorf("decode initial review metadata: %w", err)
	}
	if err := json.Unmarshal([]byte(candidateJSON), &candidate); err != nil {
		return "", "", fmt.Errorf("decode scrape candidate metadata: %w", err)
	}
	if strings.TrimSpace(candidate.Title) != "" {
		current.Title = candidate.Title
	}
	if strings.TrimSpace(candidate.Description) != "" {
		current.Description = candidate.Description
	}
	if strings.TrimSpace(candidate.Developer) != "" {
		current.Developer = candidate.Developer
	}
	if strings.TrimSpace(candidate.Publisher) != "" {
		current.Publisher = candidate.Publisher
	}
	if strings.TrimSpace(candidate.Genre) != "" {
		current.Genre = candidate.Genre
	}
	if candidate.Players != nil {
		current.Players = candidate.Players
	}
	if candidate.ReleaseYear != nil {
		current.ReleaseYear = candidate.ReleaseYear
	}
	merged, err := json.Marshal(current)
	if err != nil {
		return "", "", fmt.Errorf("encode initial review metadata: %w", err)
	}
	return string(merged), current.Title, nil
}

func firstReadyCandidateAsset(
	ctx context.Context,
	transaction *sql.Tx,
	candidateID, kind string,
) (sql.NullString, error) {
	var assetID sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT id
FROM scrape_candidate_assets
WHERE scrape_candidate_id=?
AND kind_hint=?
AND status='READY'
ORDER BY ordinal,
id
LIMIT 1
`, candidateID, kind).Scan(&assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, nil
	}
	if err != nil {
		return sql.NullString{}, fmt.Errorf("select initial review asset: %w", err)
	}
	return assetID, nil
}

type initialImportScope struct {
	itemID      string
	importJobID string
}

func loadInitialImportScope(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
) (initialImportScope, bool, error) {
	var scope initialImportScope
	var itemState string
	err := transaction.QueryRowContext(ctx, `
SELECT i.id,
i.import_job_id,
i.state
FROM metadata_scrape_runs r
JOIN import_items i ON i.id=r.import_item_id
WHERE r.id=?
`, runID).Scan(&scope.itemID, &scope.importJobID, &itemState)
	if errors.Is(err, sql.ErrNoRows) {
		return initialImportScope{}, false, nil
	}
	if err != nil {
		return initialImportScope{}, false, fmt.Errorf("load initial import scrape: %w", err)
	}
	if itemState != "SCRAPING" {
		return initialImportScope{}, false, nil
	}
	return scope, true, nil
}

func applyInitialReviewScreenshots(
	ctx context.Context,
	transaction *sql.Tx,
	draftID, candidateID string,
	now int64,
) error {
	screenshotRows, err := transaction.QueryContext(ctx, `
SELECT id,
ordinal
FROM scrape_candidate_assets
WHERE scrape_candidate_id=?
AND kind_hint='SCREENSHOT'
AND status='READY'
ORDER BY ordinal,
id
`, candidateID)
	if err != nil {
		return fmt.Errorf("select initial review screenshots: %w", err)
	}
	defer func() { cleanup.Error("close", screenshotRows.Close()) }()
	for screenshotRows.Next() {
		var assetID string
		var ordinal int
		if err := screenshotRows.Scan(&assetID, &ordinal); err != nil {
			return fmt.Errorf("scan initial review screenshot: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_draft_screenshot_assets(review_draft_id,
ordinal,
candidate_asset_id,
created_at_ms) VALUES(?,
?,
?,
?)
`, draftID, ordinal, assetID, now); err != nil {
			return fmt.Errorf("apply initial review screenshot: %w", err)
		}
	}
	if err := screenshotRows.Err(); err != nil {
		return fmt.Errorf("scan initial review screenshots: %w", err)
	}
	return nil
}

func applyInitialReviewCandidate(
	ctx context.Context,
	transaction *sql.Tx,
	runID, itemID string,
	now int64,
) error {
	var candidateID, candidateMetadata string
	err := transaction.QueryRowContext(ctx, `
SELECT c.id,
c.normalized_metadata_json
FROM scrape_candidates c
WHERE c.scrape_run_id=?
ORDER BY (SELECT count(*)
FROM scrape_candidate_hits h
WHERE h.scrape_candidate_id=c.id) DESC,
COALESCE((SELECT min(e.query_order)
FROM scrape_candidate_hits h
JOIN metadata_scrape_query_attempts q ON q.id=h.query_attempt_id
JOIN content_hash_evidence e ON e.id=q.content_hash_evidence_id
WHERE h.scrape_candidate_id=c.id), 2147483647),
c.provider_game_id,
c.id
LIMIT 1
`, runID).Scan(&candidateID, &candidateMetadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("select initial scrape candidate: %w", err)
	}

	var draftID, currentMetadata string
	if err := transaction.QueryRowContext(ctx, `
SELECT id,
metadata_json
FROM review_drafts
WHERE import_item_id=?
`, itemID).Scan(&draftID, &currentMetadata); err != nil {
		return fmt.Errorf("load initial review draft: %w", err)
	}
	mergedMetadata, title, err := mergeInitialReviewMetadata(currentMetadata, candidateMetadata)
	if err != nil {
		return err
	}
	coverID, err := firstReadyCandidateAsset(ctx, transaction, candidateID, "COVER")
	if err != nil {
		return err
	}
	backgroundID, err := firstReadyCandidateAsset(ctx, transaction, candidateID, "BACKGROUND")
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_drafts
SET selected_candidate_id=?,
cover_candidate_asset_id=?,
background_candidate_asset_id=?,
metadata_json=?,
updated_at_ms=?
WHERE id=?
`, candidateID, nullableText(coverID), nullableText(backgroundID), mergedMetadata, now, draftID); err != nil {
		return fmt.Errorf("apply initial scrape candidate: %w", err)
	}
	if err := applyInitialReviewScreenshots(ctx, transaction, draftID, candidateID, now); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET search_text=trim(search_text || ' ' || lower(?))
WHERE id=?
`, title, itemID); err != nil {
		return fmt.Errorf("update initial review search text: %w", err)
	}
	return nil
}

// completeInitialImport atomically exposes a newly imported item only after its first scrape has settled.
func (service *Service) completeInitialImport(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	now int64,
) error {
	scope, active, err := loadInitialImportScope(ctx, transaction, runID)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	if err := applyInitialReviewCandidate(ctx, transaction, runID, scope.itemID, now); err != nil {
		return err
	}

	result, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='REVIEW_PENDING',
failed_stage=NULL,
last_error_code=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='SCRAPING'
	`, now, scope.itemID)
	if err != nil {
		return fmt.Errorf("expose initial review item: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("expose initial review item: %w", errInitialItemState)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE import_jobs
SET running_item_count=running_item_count-1,
review_pending_item_count=review_pending_item_count+1,
state=CASE
WHEN running_item_count=1 AND (failed_item_count>0 OR rejected_file_count>0) THEN 'PARTIAL_FAILURE'
WHEN running_item_count=1 THEN 'REVIEW_PENDING'
ELSE 'RUNNING'
END,
version=version+1,
updated_at_ms=?
WHERE id=?
AND running_item_count>0
	`, now, scope.importJobID)
	if err != nil {
		return fmt.Errorf("update initial import progress: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("update initial import progress: %w", errInitialProgressState)
	}
	return nil
}

func (service *Service) complete(ctx context.Context, runID, jobID string, candidateCount int) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if err := service.completeInitialImport(ctx, transaction, runID, now); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE metadata_scrape_runs
SET state='COMPLETED',
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
AND state='RUNNING'
`, now, now, runID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
leased_until_ms=NULL,
heartbeat_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, now, now, jobID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	data := fmt.Sprintf(`{"candidateCount":%d}`, candidateCount)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'SUCCEEDED',
?,
?
FROM jobs
WHERE id=?
`, data, now, jobID); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit metadata asset fetch: %w", err)
	}
	return nil
}

func failInitialImport(
	ctx context.Context,
	transaction *sql.Tx,
	runID, code string,
	now int64,
) error {
	scope, active, err := loadInitialImportScope(ctx, transaction, runID)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE import_items
SET state='FAILED_RETRYABLE',
failed_stage='SCRAPING',
last_error_code=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='SCRAPING'
`, code, now, scope.itemID)
	if err != nil {
		return fmt.Errorf("fail initial import item: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("fail initial import item: %w", errInitialItemState)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE import_jobs
SET running_item_count=running_item_count-1,
failed_item_count=failed_item_count+1,
state=CASE WHEN running_item_count=1 THEN 'PARTIAL_FAILURE' ELSE 'RUNNING' END,
last_error_code=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND running_item_count>0
`, code, now, scope.importJobID)
	if err != nil {
		return fmt.Errorf("fail initial import progress: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("fail initial import progress: %w", errInitialProgressState)
	}
	return nil
}

func (service *Service) fail(ctx context.Context, runID, jobID, code string, cause error) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(
		ctx,
		`
UPDATE metadata_scrape_runs
SET state='FAILED',
error_code=?,
version=version+1,
updated_at_ms=?,
completed_at_ms=?
WHERE id=?
AND state='RUNNING'
`,
		code,
		now,
		now,
		runID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=1,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		now,
		jobID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if err := failInitialImport(ctx, transaction, runID, code, now); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) SELECT id,
scope_type,
scope_id,
'FAILED',
?,
?
FROM jobs
WHERE id=?
`,
		fmt.Sprintf(`{"code":%q}`, code),
		now,
		jobID,
	); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("%s: %w (persist failure: %w)", code, cause, err)
	}
	return fmt.Errorf("%s: %w", code, cause)
}

func nullableStatus(status int) any {
	if status == 0 {
		return nil
	}
	return status
}

func newID() string {
	value, _ := uuid.NewV7()
	return value.String()
}
