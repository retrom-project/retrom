package metadatascrape

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) schedule(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, providerName, dedupeInput string,
	bypassCache bool,
) (Scheduled, error) {
	scheduled, now, err := service.createImportSchedule(
		ctx, transaction, itemID, providerName, dedupeInput, bypassCache,
	)
	if err != nil || scheduled.Noop {
		return scheduled, err
	}
	runID := scheduled.RunID
	var platformID string
	if err := transaction.QueryRowContext(ctx, `
SELECT j.platform_id FROM import_items i JOIN import_jobs j ON j.id=i.import_job_id WHERE i.id=?
`, itemID).Scan(&platformID); err != nil {
		return Scheduled{}, fmt.Errorf("metadatascrape/service: %w", err)
	}
	if platformID == "arcade" {
		if err := service.scheduleImportArcadeEvidence(ctx, transaction, itemID, runID, now); err != nil {
			return Scheduled{}, err
		}
		return scheduled, nil
	}
	if err := service.scheduleImportContentEvidence(ctx, transaction, itemID, runID, now); err != nil {
		return Scheduled{}, err
	}
	return scheduled, nil
}

func (service *Service) createImportSchedule(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, providerName, dedupeInput string,
	bypassCache bool,
) (Scheduled, int64, error) {
	if providerName != "HASHEOUS" && providerName != "NONE" {
		return Scheduled{}, 0, errProviderInvalid
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
		return Scheduled{}, 0, fmt.Errorf("create metadata job: %w", err)
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
		return Scheduled{}, 0, fmt.Errorf("create metadata run: %w", err)
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
		return Scheduled{}, 0, fmt.Errorf("create metadata event: %w", err)
	}
	if providerName == "NONE" {
		return Scheduled{RunID: runID, JobID: jobID, Noop: true}, now, nil
	}
	return Scheduled{RunID: runID, JobID: jobID}, now, nil
}

func (service *Service) scheduleImportContentEvidence(
	ctx context.Context,
	transaction *sql.Tx,
	itemID, runID string,
	now int64,
) error {
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
		return fmt.Errorf("metadatascrape/service: %w", err)
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
			return fmt.Errorf("metadatascrape/service: %w", err)
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
				return fmt.Errorf("metadatascrape/service: %w", err)
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
				return fmt.Errorf("metadatascrape/service: %w", err)
			}
		}
		order++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("metadatascrape/service: %w", err)
	}
	return nil
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

// Contract branches stay contiguous for a single auditable decision.
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
