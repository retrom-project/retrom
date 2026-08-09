package arcadecatalog

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"

	"retrom/internal/arcadedat"
	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/datindex"
)

var (
	ErrInvalid      = errors.New("DAT_VERSION_INVALID")
	ErrDiffNotReady = errors.New("DAT_DIFF_NOT_READY")
	ErrDiffStale    = errors.New("DAT_DIFF_STALE")
)

type CreateRequest struct {
	UploadFileID   string `json:"uploadFileId"`
	CoreArtifactID string `json:"coreArtifactId"`
}

type Created struct {
	DATVersionID string `json:"datVersionId"`
	JobID        string `json:"jobId"`
	ParseStatus  string `json:"parseStatus"`
	Version      int64  `json:"version"`
}

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	now      func() time.Time
	hooks    RevalidationHooks
	diffMu   sync.Mutex
}

type RevalidationHooks struct {
	Queue  func(context.Context, *sql.Tx, string, string) (int64, error)
	Resume func()
}

func New(database *sql.DB, blobs *blobstore.Store, now func() time.Time, configured ...RevalidationHooks) *Service {
	service := &Service{database: database, blobs: blobs, now: now}
	if len(configured) > 0 {
		service.hooks = configured[0]
	}
	return service
}

//nolint:funlen // The catalog declaration and its job/audit writes share one atomic transaction and rollback boundary.
func (service *Service) Create(ctx context.Context, request CreateRequest) (Created, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var coreID string
	if err := transaction.QueryRowContext(ctx, `
SELECT core_id
FROM core_artifacts
WHERE id=?
AND enabled=1
`, request.CoreArtifactID).Scan(&coreID); err != nil ||
		coreID != "fbneo" && coreID != "mame2003" && coreID != "mame2003_plus" {
		return Created{}, ErrInvalid
	}
	var uploadID, blobID, digest string
	if err := transaction.QueryRowContext(ctx, `
SELECT f.upload_session_id,
b.id,
b.sha256
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=?
AND f.state='COMPLETE'
`, request.UploadFileID).Scan(&uploadID, &blobID, &digest); err != nil {
		return Created{}, ErrInvalid
	}
	var baseID sql.NullString
	_ = transaction.QueryRowContext(ctx, `
SELECT id
FROM dat_versions
WHERE core_artifact_id=?
AND is_active=1
`, request.CoreArtifactID).
		Scan(&baseID)
	datID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	consumptionID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	dedupe := sha256.Sum256([]byte("dat:" + request.UploadFileID + ":" + request.CoreArtifactID))
	payload, _ := json.Marshal(
		map[string]any{
			"datVersionId":   datID.String(),
			"blobId":         blobID,
			"coreArtifactId": request.CoreArtifactID,
			"coreId":         coreID,
		},
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
'DAT_VERSION',
?,
'DAT_PARSE',
?,
1,
?,
1,
'QUEUED',
0,
2,
?,
?,
?)
`, jobID.String(), datID.String(), hex.EncodeToString(dedupe[:]), string(payload), now, now, now); err != nil {
		return Created{}, ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO dat_versions(id,
core_id,
core_artifact_id,
source,
builtin_relative_path,
blob_id,
sha256,
parser_version,
compatibility_status,
parse_status,
is_active,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
'USER',
NULL,
? ,
?,
'retrom-dat-v1',
'UNKNOWN',
'PENDING',
0,
1,
?,
?)
`, datID.String(), coreID, request.CoreArtifactID, blobID, digest, now, now); err != nil {
		return Created{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO dat_import_jobs(job_id,
dat_version_id,
base_dat_version_id,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?)
`, jobID.String(), datID.String(), nullable(baseID), now, now); err != nil {
		return Created{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,
upload_session_id,
upload_file_id,
consumer_type,
consumer_id,
created_at_ms) VALUES(?,
?,
?,
'DAT_VERSION',
?,
?)
`, consumptionID.String(), uploadID, request.UploadFileID, datID.String(), now); err != nil {
		return Created{}, ErrInvalid
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	go service.parse(context.WithoutCancel(ctx), datID.String(), jobID.String(), coreID, digest)
	return Created{DATVersionID: datID.String(), JobID: jobID.String(), ParseStatus: "PENDING", Version: 1}, nil
}

func nullable(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (service *Service) parse(parent context.Context, datID, jobID, coreID, digest string) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	now := service.now().UnixMilli()
	started, err := service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='RUNNING',
attempt_count=attempt_count+1,
execution_started_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`,
		now,
		now,
		jobID,
	)
	if err != nil {
		return
	}
	if rows, _ := started.RowsAffected(); rows != 1 {
		return
	}
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE dat_versions
SET parse_status='PARSING',
version=version+1,
updated_at_ms=?
WHERE id=?
`,
		now,
		datID,
	)
	file, err := service.blobs.OpenDigest(digest)
	if err != nil {
		service.failParse(ctx, datID, jobID, "DAT_BLOB_UNAVAILABLE")
		return
	}
	catalog, parseErr := arcadedat.ParseCatalog(ctx, file, coreID)
	cleanup.Error("close", file.Close())
	if parseErr != nil {
		service.failParse(ctx, datID, jobID, stableParseCode(parseErr))
		return
	}
	var jobState string
	if err := service.database.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&jobState); err != nil {
		return
	}
	if jobState == "CANCEL_REQUESTED" {
		now = service.now().UnixMilli()
		_, _ = service.database.ExecContext(
			ctx,
			`
UPDATE dat_versions
SET parse_status='CANCELLED',
version=version+1,
updated_at_ms=?
WHERE id=?
AND parse_status='PARSING';
 UPDATE jobs
SET state='CANCELLED',
finished_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='CANCEL_REQUESTED'
`,
			now,
			datID,
			now,
			now,
			jobID,
		)
		return
	}
	stats := catalog.Stats
	now = service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if err := service.replaceCatalog(ctx, transaction, datID, catalog); err != nil {
		cleanup.Rollback(transaction)
		service.failParse(ctx, datID, jobID, "DAT_INDEX_WRITE_FAILED")
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET parse_status='READY',
machine_count=?,
rom_entry_count=?,
disk_entry_count=?,
bios_set_count=?,
default_bios_set_count=?,
explicit_bios_machine_count=?,
base_dependency_target_count=?,
unresolved_relation_count=?,
version=version+1,
updated_at_ms=?,
parsed_at_ms=?
WHERE id=?
`,
		stats.MachineCount,
		stats.ROMEntryCount,
		stats.DiskEntryCount,
		stats.BIOSSetCount,
		stats.DefaultBIOSSetCount,
		stats.ExplicitBIOSMachineCount,
		stats.BaseDependencyTargetCount,
		stats.UnresolvedCloneofTargetCount+stats.UnresolvedRomofTargetCount,
		now,
		now,
		datID,
	); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, now, jobID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'SUCCEEDED',
json_object('machineCount',
?,
'romEntryCount',
?,
'diskEntryCount',
?),
?)
`, jobID, datID, stats.MachineCount, stats.ROMEntryCount, stats.DiskEntryCount, now); err != nil {
		return
	}
	if err := transaction.Commit(); err != nil {
		cleanup.Rollback(transaction)
		service.failParse(ctx, datID, jobID, "DAT_INDEX_WRITE_FAILED")
		return
	}
	_, _ = service.ScheduleDiff(context.WithoutCancel(ctx), datID)
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func (service *Service) replaceCatalog(
	ctx context.Context,
	transaction *sql.Tx,
	datID string,
	catalog arcadedat.Catalog,
) error {
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM dat_machines
WHERE dat_version_id=?
`, datID); err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	for _, machine := range catalog.Machines {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO dat_machines(dat_version_id,
machine_name,
description,
year,
manufacturer,
cloneof,
romof,
is_explicit_bios,
classification) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
			datID,
			machine.Name,
			machine.Description,
			machine.Year,
			machine.Manufacturer,
			nullableText(machine.CloneOf),
			nullableText(machine.ROMOf),
			boolInteger(machine.ExplicitBIOS),
			machine.Classification,
		); err != nil {
			return fmt.Errorf("arcadecatalog/service: %w", err)
		}
		for _, bios := range machine.BIOSSets {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO dat_bios_sets(dat_version_id,
machine_name,
bios_name,
description,
is_default) VALUES(?,
?,
?,
?,
?)
`, datID, machine.Name, bios.Name, bios.Description, boolInteger(bios.Default)); err != nil {
				return fmt.Errorf("arcadecatalog/service: %w", err)
			}
		}
		for _, rom := range machine.ROMs {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO dat_rom_entries(dat_version_id,
machine_name,
ordinal,
name,
size_bytes,
crc32,
sha1,
status,
merge_name,
bios_name) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
				datID,
				machine.Name,
				rom.Ordinal,
				rom.Name,
				rom.SizeBytes,
				nullableText(rom.CRC32),
				nullableText(rom.SHA1),
				rom.Status,
				nullableText(rom.MergeName),
				nullableText(rom.BIOSName),
			); err != nil {
				return fmt.Errorf("arcadecatalog/service: %w", err)
			}
		}
		for _, disk := range machine.Disks {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO dat_disk_entries(dat_version_id,
machine_name,
ordinal,
name,
sha1,
status) VALUES(?,
?,
?,
?,
?,
?)
`, datID, machine.Name, disk.Ordinal, disk.Name, nullableText(disk.SHA1), disk.Status); err != nil {
				return fmt.Errorf("arcadecatalog/service: %w", err)
			}
		}
	}
	return nil
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stableParseCode(err error) string {
	switch {
	case errors.Is(err, arcadedat.ErrUnsafeDTD):
		return "DAT_UNSAFE_DTD"
	case errors.Is(err, arcadedat.ErrLimitExceeded):
		return "DAT_LIMIT_EXCEEDED"
	default:
		return "DAT_INVALID_DOCUMENT"
	}
}

func (service *Service) failParse(ctx context.Context, datID, jobID, code string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE dat_versions
SET parse_status='FAILED',
version=version+1,
updated_at_ms=?
WHERE id=?
`,
		now,
		datID,
	)
	_, _ = service.database.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=0,
finished_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`,
		code,
		now,
		now,
		jobID,
	)
}

type Diff struct {
	BaseDATVersionID   any            `json:"baseDatVersionId"`
	TargetDATVersionID string         `json:"targetDatVersionId"`
	Summary            map[string]any `json:"summary"`
	Items              []DiffItem     `json:"items"`
	NextCursor         any            `json:"nextCursor"`
	Impact             map[string]any `json:"impact"`
	ImpactDigest       string         `json:"impactDigest"`
	LastCursorKey      string         `json:"-"`
	HasMore            bool           `json:"-"`
}

type DiffOptions struct {
	Section string
	Change  string
	After   string
	Limit   int
}

type DiffItem struct {
	Section string         `json:"section"`
	Change  string         `json:"change"`
	Key     map[string]any `json:"key"`
	Before  map[string]any `json:"before"`
	After   map[string]any `json:"after"`
	cursor  string
}

type changeCounts struct {
	Added   int64 `json:"added"`
	Removed int64 `json:"removed"`
	Changed int64 `json:"changed"`
}

func (counts *changeCounts) add(change string) {
	switch change {
	case "ADDED":
		counts.Added++
	case "REMOVED":
		counts.Removed++
	case "CHANGED":
		counts.Changed++
	}
}

type DiffJob struct {
	DATVersionID string `json:"datVersionId"`
	JobID        string `json:"jobId"`
	State        string `json:"state"`
	Version      int64  `json:"version"`
}

type diffSnapshotContext struct {
	inputDigest string
	baseID      sql.NullString
}

func (service *Service) diffPlatformInputs(ctx context.Context, artifactID string) ([]map[string]any, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT p.id,
p.version
FROM platform_instances p
JOIN core_artifacts a ON a.core_id=p.default_core_id
WHERE a.id=?
AND p.enabled=1
AND p.deleted_at_ms IS NULL
ORDER BY p.id
`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id string
		var version int64
		if err := rows.Scan(&id, &version); err != nil {
			return nil, fmt.Errorf("arcadecatalog/service: %w", err)
		}
		items = append(items, map[string]any{"id": id, "version": version})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return items, nil
}

func (service *Service) diffVariantInputs(ctx context.Context, artifactID string) ([]map[string]any, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT v.id,
v.current_revision_id,
v.version,
g.current_content_revision_id
FROM game_variants v
JOIN game_variant_revisions r ON r.id=v.current_revision_id
JOIN games g ON g.id=v.game_id
WHERE r.core_artifact_id=?
AND g.status='PUBLISHED'
ORDER BY v.id
`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, revisionID, contentID string
		var version int64
		if err := rows.Scan(&id, &revisionID, &version, &contentID); err != nil {
			return nil, fmt.Errorf("arcadecatalog/service: %w", err)
		}
		items = append(items, map[string]any{
			"contentRevisionId": contentID,
			"id":                id,
			"revisionId":        revisionID,
			"version":           version,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return items, nil
}

type diffVariantImpactInput struct {
	id                string
	revisionID        string
	status            string
	compatibilityCode string
	contentID         string
	version           int64
}

func (service *Service) diffVariantImpactInputs(
	ctx context.Context,
	artifactID string,
) ([]diffVariantImpactInput, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT v.id,
v.current_revision_id,
v.version,
r.status,
r.compatibility_code,
g.current_content_revision_id
FROM game_variants v
JOIN game_variant_revisions r ON r.id=v.current_revision_id
JOIN games g ON g.id=v.game_id
WHERE r.core_artifact_id=?
AND g.status='PUBLISHED'
ORDER BY v.id
`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]diffVariantImpactInput, 0)
	for rows.Next() {
		var item diffVariantImpactInput
		if err := rows.Scan(
			&item.id,
			&item.revisionID,
			&item.version,
			&item.status,
			&item.compatibilityCode,
			&item.contentID,
		); err != nil {
			return nil, fmt.Errorf("arcadecatalog/service: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return items, nil
}

func normalizedDiffOptions(requested []DiffOptions) (DiffOptions, error) {
	options := DiffOptions{Section: "MACHINES", Change: "ALL", Limit: 50}
	if len(requested) > 0 {
		options = requested[0]
	}
	if options.Section == "" {
		options.Section = "MACHINES"
	}
	if options.Change == "" {
		options.Change = "ALL"
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	if options.Limit < 1 || options.Limit > 100 ||
		!validDiffValue(options.Section, "MACHINES", "ROM_ENTRIES", "BIOS_SETS", "DEPENDENCY_TARGETS") ||
		!validDiffValue(options.Change, "ALL", "ADDED", "REMOVED", "CHANGED") {
		return DiffOptions{}, ErrInvalid
	}
	return options, nil
}

func (service *Service) currentDiffContext(ctx context.Context, datID string) (diffSnapshotContext, error) {
	var artifactID, targetSHA, targetParser, parseStatus string
	var active, artifactVersion int64
	if err := service.database.QueryRowContext(ctx, `
SELECT d.core_artifact_id,
d.sha256,
d.parser_version,
d.parse_status,
d.is_active,
a.version
FROM dat_versions d
JOIN core_artifacts a ON a.id=d.core_artifact_id
WHERE d.id=?
`, datID).Scan(&artifactID, &targetSHA, &targetParser, &parseStatus, &active, &artifactVersion); err != nil ||
		parseStatus != "READY" || active == 1 {
		return diffSnapshotContext{}, ErrInvalid
	}
	var baseID, baseSHA, baseParser sql.NullString
	err := service.database.QueryRowContext(ctx, `
SELECT id,
sha256,
parser_version
FROM dat_versions
WHERE core_artifact_id=?
AND is_active=1
`, artifactID).Scan(&baseID, &baseSHA, &baseParser)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return diffSnapshotContext{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	platforms, err := service.diffPlatformInputs(ctx, artifactID)
	if err != nil {
		return diffSnapshotContext{}, err
	}
	variants, err := service.diffVariantInputs(ctx, artifactID)
	if err != nil {
		return diffSnapshotContext{}, err
	}
	encoded, _ := json.Marshal(map[string]any{
		"baseDatVersionId":    nullable(baseID),
		"baseParserVersion":   nullable(baseParser),
		"baseSha256":          nullable(baseSHA),
		"coreArtifactId":      artifactID,
		"coreArtifactVersion": artifactVersion,
		"platformInstances":   platforms,
		"schemaVersion":       1,
		"targetDatVersionId":  datID,
		"targetParserVersion": targetParser,
		"targetSha256":        targetSHA,
		"variants":            variants,
	})
	digest := sha256.Sum256(encoded)
	return diffSnapshotContext{inputDigest: hex.EncodeToString(digest[:]), baseID: baseID}, nil
}

// ScheduleDiff discards any previous materialization and queues a fresh comparison.
func (service *Service) ScheduleDiff(ctx context.Context, datID string) (DiffJob, error) {
	inputs, err := service.currentDiffContext(ctx, datID)
	if err != nil {
		return DiffJob{}, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return DiffJob{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var snapshotID, state, existingDigest string
	var version int64
	err = transaction.QueryRowContext(ctx, `
SELECT id,
state,
input_digest,
version
FROM dat_diff_snapshots
WHERE dat_version_id=?
`, datID).Scan(&snapshotID, &state, &existingDigest, &version)
	if err == nil && (state == "PENDING" || state == "RUNNING") && existingDigest == inputs.inputDigest {
		if err := transaction.Commit(); err != nil {
			return DiffJob{}, fmt.Errorf("arcadecatalog/service: %w", err)
		}
		return DiffJob{DATVersionID: datID, JobID: snapshotID, State: state, Version: version}, nil
	}
	now := service.now().UnixMilli()
	switch {
	case errors.Is(err, sql.ErrNoRows):
		created, _ := uuid.NewV7()
		snapshotID = created.String()
		version = 1
		if err := createDiffSnapshot(ctx, transaction, snapshotID, datID, inputs, now); err != nil {
			return DiffJob{}, err
		}
	case err == nil:
		version++
		if err := resetDiffSnapshot(ctx, transaction, snapshotID, inputs, version, now); err != nil {
			return DiffJob{}, err
		}
	default:
		return DiffJob{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return DiffJob{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	go service.runDiff(context.WithoutCancel(ctx), snapshotID, datID, inputs.inputDigest)
	return DiffJob{DATVersionID: datID, JobID: snapshotID, State: "PENDING", Version: version}, nil
}

func createDiffSnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	snapshotID, datID string,
	inputs diffSnapshotContext,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO dat_diff_snapshots(id,
dat_version_id,
base_dat_version_id,
state,
input_digest,
attempt_count,
version,
queued_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
'PENDING',
?,
0,
1,
?,
?,
?)
`, snapshotID, datID, nullable(inputs.baseID), inputs.inputDigest, now, now, now); err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return nil
}

func resetDiffSnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	snapshotID string,
	inputs diffSnapshotContext,
	version, now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM dat_diff_items
WHERE snapshot_id=?
`, snapshotID); err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_diff_snapshots
SET base_dat_version_id=?,
state='PENDING',
input_digest=?,
summary_json=NULL,
impact_json=NULL,
impact_digest=NULL,
error_code=NULL,
attempt_count=0,
version=?,
queued_at_ms=?,
started_at_ms=NULL,
completed_at_ms=NULL,
updated_at_ms=?
WHERE id=?
`, nullable(inputs.baseID), inputs.inputDigest, version, now, now, snapshotID); err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return nil
}

func (service *Service) ResumeDiffJobs() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = service.recoverInterruptedDiffs(ctx)
	missing, err := service.missingDiffSnapshotIDs(ctx)
	if err == nil {
		for _, datID := range missing {
			_, _ = service.ScheduleDiff(ctx, datID)
		}
	}
	pending, err := service.pendingDiffSnapshots(ctx)
	if err != nil {
		return
	}
	for _, item := range pending {
		go service.runDiff(context.Background(), item[0], item[1], item[2])
	}
}

func (service *Service) recoverInterruptedDiffs(ctx context.Context) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM dat_diff_items
WHERE snapshot_id IN (SELECT id FROM dat_diff_snapshots WHERE state='RUNNING')
`); err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_diff_snapshots
SET state='PENDING',
started_at_ms=NULL,
version=version+1,
queued_at_ms=?,
updated_at_ms=?
WHERE state='RUNNING'
	`, now, now); err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return nil
}

func (service *Service) missingDiffSnapshotIDs(ctx context.Context) ([]string, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT d.id
FROM dat_versions d
LEFT JOIN dat_diff_snapshots s ON s.dat_version_id=d.id
WHERE d.source='USER'
AND d.parse_status='READY'
AND d.is_active=0
AND s.id IS NULL
ORDER BY d.id
`)
	if err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]string, 0)
	for rows.Next() {
		var datID string
		if err := rows.Scan(&datID); err != nil {
			return nil, fmt.Errorf("arcadecatalog/service: %w", err)
		}
		items = append(items, datID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return items, nil
}

func (service *Service) pendingDiffSnapshots(ctx context.Context) ([][3]string, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,
dat_version_id,
input_digest
FROM dat_diff_snapshots
WHERE state='PENDING'
ORDER BY queued_at_ms,
id
`)
	if err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	pending := make([][3]string, 0)
	for rows.Next() {
		var item [3]string
		if err := rows.Scan(&item[0], &item[1], &item[2]); err != nil {
			return nil, fmt.Errorf("arcadecatalog/service: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return pending, nil
}

func (service *Service) runDiff(parent context.Context, snapshotID, datID, inputDigest string) {
	service.diffMu.Lock()
	defer service.diffMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	now := service.now().UnixMilli()
	claimed, err := service.database.ExecContext(ctx, `
UPDATE dat_diff_snapshots
SET state='RUNNING',
attempt_count=attempt_count+1,
started_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND dat_version_id=?
AND input_digest=?
AND state='PENDING'
`, now, now, snapshotID, datID, inputDigest)
	if err != nil {
		return
	}
	if count, _ := claimed.RowsAffected(); count != 1 {
		return
	}
	diff, err := service.computeDiff(ctx, datID)
	if err != nil {
		service.failDiff(ctx, snapshotID, inputDigest, "DAT_DIFF_COMPUTE_FAILED")
		return
	}
	if err := service.materializeDiffSections(ctx, snapshotID, datID, inputDigest, diff); err != nil {
		if !errors.Is(err, ErrDiffStale) {
			service.failDiff(ctx, snapshotID, inputDigest, "DAT_DIFF_WRITE_FAILED")
		}
		return
	}
	latest, err := service.currentDiffContext(ctx, datID)
	if err != nil || latest.inputDigest != inputDigest {
		service.staleDiff(ctx, snapshotID, inputDigest)
		return
	}
	if err := service.completeDiff(ctx, snapshotID, datID, inputDigest, diff); err != nil {
		service.failDiff(ctx, snapshotID, inputDigest, "DAT_DIFF_WRITE_FAILED")
	}
}

func (service *Service) materializeDiffSections(
	ctx context.Context,
	snapshotID, datID, inputDigest string,
	diff Diff,
) error {
	sections := []string{"MACHINES", "ROM_ENTRIES", "BIOS_SETS", "DEPENDENCY_TARGETS"}
	for _, section := range sections {
		items := make([]DiffItem, 0)
		if err := service.scanDiffSection(ctx, stringValue(diff.BaseDATVersionID), datID, section, func(item DiffItem) {
			items = append(items, item)
		}); err != nil {
			return fmt.Errorf("arcadecatalog/service: %w", err)
		}
		if err := service.writeDiffItems(ctx, snapshotID, inputDigest, items); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) completeDiff(
	ctx context.Context,
	snapshotID, datID, inputDigest string,
	diff Diff,
) error {
	summary, _ := json.Marshal(diff.Summary)
	impact, _ := json.Marshal(diff.Impact)
	now := service.now().UnixMilli()
	result, err := service.database.ExecContext(ctx, `
UPDATE dat_diff_snapshots
SET state='READY',
summary_json=?,
impact_json=?,
impact_digest=?,
error_code=NULL,
completed_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND input_digest=?
AND state='RUNNING'
`, string(summary), string(impact), diff.ImpactDigest, now, now, snapshotID, inputDigest)
	if err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrDiffStale
	}
	_, _ = service.database.ExecContext(ctx, `
UPDATE dat_import_jobs
SET diff_summary_json=?,
diff_input_digest=?,
updated_at_ms=?
WHERE dat_version_id=?
`, string(summary), inputDigest, now, datID)
	return nil
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func (service *Service) writeDiffItems(
	ctx context.Context,
	snapshotID, inputDigest string,
	items []DiffItem,
) error {
	const batchSize = 1000
	for start := 0; start < len(items); start += batchSize {
		end := min(start+batchSize, len(items))
		if err := service.writeDiffBatch(ctx, snapshotID, inputDigest, items[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) writeDiffBatch(
	ctx context.Context,
	snapshotID, inputDigest string,
	items []DiffItem,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state, currentDigest string
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
input_digest
FROM dat_diff_snapshots
WHERE id=?
`, snapshotID).Scan(&state, &currentDigest); err != nil || state != "RUNNING" || currentDigest != inputDigest {
		return ErrDiffStale
	}
	statement, err := transaction.PrepareContext(ctx, `
INSERT INTO dat_diff_items(snapshot_id,
section,
cursor_key,
change_kind,
key_json,
before_json,
after_json) VALUES(?,
?,
?,
?,
?,
?,
?)
`)
	if err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", statement.Close()) }()
	for _, item := range items {
		keyJSON, _ := json.Marshal(item.Key)
		beforeJSON, _ := json.Marshal(item.Before)
		afterJSON, _ := json.Marshal(item.After)
		if _, err := statement.ExecContext(
			ctx,
			snapshotID,
			item.Section,
			item.cursor,
			item.Change,
			string(keyJSON),
			string(beforeJSON),
			string(afterJSON),
		); err != nil {
			return fmt.Errorf("arcadecatalog/service: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return nil
}

func (service *Service) failDiff(ctx context.Context, snapshotID, inputDigest, code string) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM dat_diff_items
WHERE snapshot_id=?
`, snapshotID); err != nil {
		return
	}
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_diff_snapshots
SET state='FAILED',
summary_json=NULL,
impact_json=NULL,
impact_digest=NULL,
error_code=?,
completed_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND input_digest=?
AND state='RUNNING'
`, code, now, now, snapshotID, inputDigest); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) staleDiff(ctx context.Context, snapshotID, inputDigest string) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM dat_diff_items
WHERE snapshot_id=?
`, snapshotID); err != nil {
		return
	}
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_diff_snapshots
SET state='STALE',
summary_json=NULL,
impact_json=NULL,
impact_digest=NULL,
error_code=NULL,
completed_at_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
AND input_digest=?
`, now, snapshotID, inputDigest); err != nil {
		return
	}
	_ = transaction.Commit()
}

type readyDiffSnapshot struct {
	id           string
	inputDigest  string
	baseID       sql.NullString
	summary      map[string]any
	impact       map[string]any
	impactDigest string
}

func (service *Service) loadReadyDiffSnapshot(ctx context.Context, datID string) (readyDiffSnapshot, error) {
	var snapshot readyDiffSnapshot
	var state string
	var summaryJSON, impactJSON, impactDigest sql.NullString
	err := service.database.QueryRowContext(ctx, `
SELECT s.id,
s.base_dat_version_id,
s.state,
s.input_digest,
s.summary_json,
s.impact_json,
s.impact_digest
FROM dat_diff_snapshots s
JOIN dat_versions d ON d.id=s.dat_version_id
WHERE s.dat_version_id=?
AND d.parse_status='READY'
`, datID).Scan(
		&snapshot.id,
		&snapshot.baseID,
		&state,
		&snapshot.inputDigest,
		&summaryJSON,
		&impactJSON,
		&impactDigest,
	)
	if err != nil {
		return readyDiffSnapshot{}, ErrDiffStale
	}
	if state == "PENDING" || state == "RUNNING" {
		return readyDiffSnapshot{}, ErrDiffNotReady
	}
	if state != "READY" || !summaryJSON.Valid || !impactJSON.Valid || !impactDigest.Valid {
		return readyDiffSnapshot{}, ErrDiffStale
	}
	latest, err := service.currentDiffContext(ctx, datID)
	if err != nil || latest.inputDigest != snapshot.inputDigest {
		return readyDiffSnapshot{}, ErrDiffStale
	}
	snapshot.summary = map[string]any{}
	snapshot.impact = map[string]any{}
	if json.Unmarshal([]byte(summaryJSON.String), &snapshot.summary) != nil ||
		json.Unmarshal([]byte(impactJSON.String), &snapshot.impact) != nil {
		return readyDiffSnapshot{}, ErrDiffStale
	}
	snapshot.impactDigest = impactDigest.String
	return snapshot, nil
}

func (service *Service) readMaterializedDiffItems(
	ctx context.Context,
	snapshotID string,
	options DiffOptions,
) ([]DiffItem, error) {
	arguments := []any{snapshotID, options.Section, options.After}
	changeFilter := ""
	if options.Change != "ALL" {
		changeFilter = " AND change_kind=?"
		arguments = append(arguments, options.Change)
	}
	arguments = append(arguments, options.Limit+1)
	rows, err := service.database.QueryContext(ctx, `
SELECT section,
change_kind,
cursor_key,
key_json,
before_json,
after_json
FROM dat_diff_items
WHERE snapshot_id=?
AND section=?
AND cursor_key>?
`+changeFilter+`
ORDER BY cursor_key
LIMIT ?
`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]DiffItem, 0, options.Limit+1)
	for rows.Next() {
		var item DiffItem
		var keyJSON, beforeJSON, afterJSON string
		if err := rows.Scan(&item.Section, &item.Change, &item.cursor, &keyJSON, &beforeJSON, &afterJSON); err != nil {
			return nil, fmt.Errorf("arcadecatalog/service: %w", err)
		}
		if json.Unmarshal([]byte(keyJSON), &item.Key) != nil ||
			json.Unmarshal([]byte(beforeJSON), &item.Before) != nil ||
			json.Unmarshal([]byte(afterJSON), &item.After) != nil {
			return nil, ErrDiffStale
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	return items, nil
}

// Diff reads only a completed materialization; it never scans the raw DAT indexes on an HTTP request.
func (service *Service) Diff(ctx context.Context, datID string, requested ...DiffOptions) (Diff, error) {
	options, err := normalizedDiffOptions(requested)
	if err != nil {
		return Diff{}, err
	}
	snapshot, err := service.loadReadyDiffSnapshot(ctx, datID)
	if err != nil {
		return Diff{}, err
	}
	items, err := service.readMaterializedDiffItems(ctx, snapshot.id, options)
	if err != nil {
		return Diff{}, err
	}
	result := Diff{
		BaseDATVersionID:   nullable(snapshot.baseID),
		TargetDATVersionID: datID,
		Summary:            snapshot.summary,
		Items:              items,
		NextCursor:         nil,
		Impact:             snapshot.impact,
		ImpactDigest:       snapshot.impactDigest,
	}
	if len(result.Items) > options.Limit {
		result.Items = result.Items[:options.Limit]
		result.HasMore = true
	}
	if len(result.Items) > 0 {
		result.LastCursorKey = result.Items[len(result.Items)-1].cursor
	}
	return result, nil
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) computeDiff(ctx context.Context, datID string, requested ...DiffOptions) (Diff, error) {
	options, err := normalizedDiffOptions(requested)
	if err != nil {
		return Diff{}, err
	}
	var artifactID, status, targetSHA, targetParser string
	var unresolvedWarnings int64
	if err := service.database.QueryRowContext(ctx, `
SELECT core_artifact_id,
parse_status,
sha256,
parser_version,
COALESCE(unresolved_relation_count,
0)
FROM dat_versions
WHERE id=?
`, datID).Scan(&artifactID, &status, &targetSHA, &targetParser, &unresolvedWarnings); err != nil ||
		status != "READY" {
		return Diff{}, ErrInvalid
	}
	var baseID sql.NullString
	var baseSHA, baseParser sql.NullString
	err = service.database.QueryRowContext(ctx, `
SELECT id,
sha256,
parser_version
FROM dat_versions
WHERE core_artifact_id=?
AND is_active=1
`, artifactID).
		Scan(&baseID, &baseSHA, &baseParser)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Diff{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	base := ""
	if baseID.Valid {
		base = baseID.String
	}
	sections := []string{"MACHINES", "ROM_ENTRIES", "BIOS_SETS", "DEPENDENCY_TARGETS"}
	counts := map[string]*changeCounts{}
	selected := make([]DiffItem, 0, options.Limit+1)
	for _, section := range sections {
		sectionCounts := &changeCounts{}
		counts[section] = sectionCounts
		scanErr := service.scanDiffSection(ctx, base, datID, section, func(item DiffItem) {
			sectionCounts.add(item.Change)
			if section != options.Section || options.Change != "ALL" && options.Change != item.Change ||
				item.cursor <= options.After ||
				len(selected) >= options.Limit+1 {
				return
			}
			selected = append(selected, item)
		})
		if scanErr != nil {
			return Diff{}, scanErr
		}
	}
	summary := map[string]any{
		"schemaVersion":     1,
		"machines":          counts["MACHINES"],
		"romEntries":        counts["ROM_ENTRIES"],
		"biosSets":          counts["BIOS_SETS"],
		"dependencyTargets": counts["DEPENDENCY_TARGETS"],
		"warnings":          unresolvedWarnings,
	}
	var directories int64
	_ = service.database.QueryRowContext(ctx, `
SELECT count(*)
FROM platform_instances p
JOIN core_artifacts a ON a.core_id=p.default_core_id
WHERE a.id=?
AND p.enabled=1
AND p.deleted_at_ms IS NULL
`, artifactID).
		Scan(&directories)
	variantInputs, err := service.diffVariantImpactInputs(ctx, artifactID)
	if err != nil {
		return Diff{}, err
	}
	variantItems := make([]map[string]any, 0, len(variantInputs))
	blockedVariants := int64(0)
	for _, variant := range variantInputs {
		input, _ := json.Marshal(
			map[string]any{
				"coreArtifactId":        artifactID,
				"datVersionId":          datID,
				"gameContentRevisionId": variant.contentID,
				"schemaVersion":         1,
			},
		)
		inputDigest := sha256.Sum256(input)
		projectedStatus := "NEEDS_VALIDATION"
		var projectedCode any
		var existingStatus, existingCode string
		if queryErr := service.database.QueryRowContext(ctx, `
SELECT status,
compatibility_code
FROM game_variant_revisions
WHERE game_variant_id=?
AND validation_input_digest=?
`, variant.id, hex.EncodeToString(inputDigest[:])).Scan(&existingStatus, &existingCode); queryErr == nil {
			projectedStatus, projectedCode = existingStatus, existingCode
			if existingStatus != "READY" {
				blockedVariants++
			}
		} else if !errors.Is(queryErr, sql.ErrNoRows) {
			return Diff{}, fmt.Errorf("arcadecatalog/service: %w", queryErr)
		}
		variantItems = append(
			variantItems,
			map[string]any{
				"gameVariantId":              variant.id,
				"currentRevisionId":          variant.revisionID,
				"version":                    variant.version,
				"currentStatus":              variant.status,
				"currentCompatibilityCode":   variant.compatibilityCode,
				"projectedStatus":            projectedStatus,
				"projectedCompatibilityCode": projectedCode,
				"validationInputDigest":      hex.EncodeToString(inputDigest[:]),
			},
		)
	}
	requirementChanges := counts["DEPENDENCY_TARGETS"].Added +
		counts["DEPENDENCY_TARGETS"].Removed +
		counts["DEPENDENCY_TARGETS"].Changed
	impact := map[string]any{
		"coreArtifactId":                 artifactID,
		"dependentPlatformInstanceCount": directories,
		"variantRevalidationCount":       int64(len(variantItems)),
		"blockedVariantCount":            blockedVariants,
		"variantRevalidations":           variantItems,
		"requirementSlotChangeCount":     requirementChanges,
		"targetVersion":                  datID,
	}
	principal, _ := authn.PrincipalFromContext(ctx)
	digestInput, _ := json.Marshal(
		map[string]any{
			"action":              "DAT_ACTIVATE",
			"principalId":         principal.UserID,
			"baseDatVersionId":    nullable(baseID),
			"baseSha256":          nullable(baseSHA),
			"baseParserVersion":   nullable(baseParser),
			"impact":              impact,
			"summary":             summary,
			"targetDatVersionId":  datID,
			"targetSha256":        targetSHA,
			"targetParserVersion": targetParser,
		},
	)
	digest := sha256.Sum256(digestInput)
	result := Diff{
		BaseDATVersionID:   nullable(baseID),
		TargetDATVersionID: datID,
		Summary:            summary,
		Items:              selected,
		NextCursor:         nil,
		Impact:             impact,
		ImpactDigest:       base64.RawURLEncoding.EncodeToString(digest[:]),
	}
	if len(result.Items) > options.Limit {
		result.Items = result.Items[:options.Limit]
		result.HasMore = true
	}
	if len(result.Items) > 0 {
		result.LastCursorKey = result.Items[len(result.Items)-1].cursor
	}
	return result, nil
}

func validDiffValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func classifyDiff(before, after map[string]any) string {
	if before == nil {
		return "ADDED"
	}
	if after == nil {
		return "REMOVED"
	}
	if !reflect.DeepEqual(before, after) {
		return "CHANGED"
	}
	return ""
}

func nullableValue(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullableInteger(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func (service *Service) scanDiffSection(
	ctx context.Context,
	baseID, targetID, section string,
	visit func(DiffItem),
) error {
	switch section {
	case "MACHINES":
		return service.scanMachineDiff(ctx, baseID, targetID, false, visit)
	case "DEPENDENCY_TARGETS":
		return service.scanMachineDiff(ctx, baseID, targetID, true, visit)
	case "ROM_ENTRIES":
		return service.scanROMDiff(ctx, baseID, targetID, visit)
	case "BIOS_SETS":
		return service.scanBIOSDiff(ctx, baseID, targetID, visit)
	default:
		return ErrInvalid
	}
}

func (service *Service) scanMachineDiff(
	ctx context.Context,
	baseID, targetID string,
	dependenciesOnly bool,
	visit func(DiffItem),
) error {
	after := ""
	for {
		count, last, err := service.scanMachineDiffBatch(ctx, baseID, targetID, after, dependenciesOnly, visit)
		if err != nil {
			return err
		}
		if count < diffScanBatchSize {
			return nil
		}
		after = last
		if err := yieldDiffScan(ctx); err != nil {
			return err
		}
	}
}

const diffScanBatchSize = 500

func yieldDiffScan(ctx context.Context) error {
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("arcadecatalog/service: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

//nolint:funlen // One row contains paired before/after machine projections and is decoded atomically.
func (service *Service) scanMachineDiffBatch(
	ctx context.Context,
	baseID, targetID, after string,
	dependenciesOnly bool,
	visit func(DiffItem),
) (int, string, error) {
	rows, err := service.database.QueryContext(ctx, `
WITH b AS (
SELECT * FROM dat_machines
WHERE dat_version_id=?
AND machine_name COLLATE BINARY>?
ORDER BY machine_name COLLATE BINARY
LIMIT ?
), t AS (
SELECT * FROM dat_machines
WHERE dat_version_id=?
AND machine_name COLLATE BINARY>?
ORDER BY machine_name COLLATE BINARY
LIMIT ?
) SELECT COALESCE(b.machine_name,t.machine_name),
b.machine_name,
b.description,
b.year,
b.manufacturer,
b.cloneof,
b.romof,
b.is_explicit_bios,
b.classification,
t.machine_name,
t.description,
t.year,
t.manufacturer,
t.cloneof,
t.romof,
t.is_explicit_bios,
t.classification
FROM b
FULL OUTER JOIN t ON t.machine_name=b.machine_name
ORDER BY COALESCE(b.machine_name,t.machine_name) COLLATE BINARY
LIMIT ?
`, baseID, after, diffScanBatchSize, targetID, after, diffScanBatchSize, diffScanBatchSize)
	if err != nil {
		return 0, "", fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	count := 0
	last := after
	for rows.Next() {
		var key string
		var values [16]sql.NullString
		var baseExplicit, targetExplicit sql.NullInt64
		if err := rows.Scan(
			&key,
			&values[0],
			&values[1],
			&values[2],
			&values[3],
			&values[4],
			&values[5],
			&baseExplicit,
			&values[6],
			&values[7],
			&values[8],
			&values[9],
			&values[10],
			&values[11],
			&values[12],
			&targetExplicit,
			&values[13],
		); err != nil {
			return 0, "", fmt.Errorf("arcadecatalog/service: %w", err)
		}
		count++
		last = key
		before := machineObject(
			values[0],
			values[1],
			values[2],
			values[3],
			values[4],
			values[5],
			baseExplicit,
			values[6],
		)
		after := machineObject(
			values[7],
			values[8],
			values[9],
			values[10],
			values[11],
			values[12],
			targetExplicit,
			values[13],
		)
		if dependenciesOnly {
			before = dependencyObject(before)
			after = dependencyObject(after)
		}
		change := classifyDiff(before, after)
		if change == "" {
			continue
		}
		name := "MACHINES"
		if dependenciesOnly {
			name = "DEPENDENCY_TARGETS"
		}
		visit(
			DiffItem{
				Section: name,
				Change:  change,
				Key:     map[string]any{"machineName": key},
				Before:  before,
				After:   after,
				cursor:  key,
			},
		)
	}
	if err := rows.Err(); err != nil {
		return 0, "", fmt.Errorf("scan machine diff: %w", err)
	}
	return count, last, nil
}

func machineObject(
	exists, description, year, manufacturer, cloneof, romof sql.NullString,
	explicit sql.NullInt64,
	classification sql.NullString,
) map[string]any {
	if !exists.Valid {
		return nil
	}
	return map[string]any{
		"description":    description.String,
		"year":           year.String,
		"manufacturer":   manufacturer.String,
		"cloneof":        nullableValue(cloneof),
		"romof":          nullableValue(romof),
		"isExplicitBios": explicit.Int64 == 1,
		"classification": classification.String,
	}
}

func dependencyObject(machine map[string]any) map[string]any {
	if machine == nil || machine["classification"] == "NORMAL" && machine["cloneof"] == nil && machine["romof"] == nil {
		return nil
	}
	return map[string]any{
		"cloneof":        machine["cloneof"],
		"romof":          machine["romof"],
		"classification": machine["classification"],
	}
}

func (service *Service) scanROMDiff(ctx context.Context, baseID, targetID string, visit func(DiffItem)) error {
	afterMachine := ""
	afterOrdinal := int64(-1)
	for {
		count, machine, ordinal, err := service.scanROMDiffBatch(
			ctx,
			baseID,
			targetID,
			afterMachine,
			afterOrdinal,
			visit,
		)
		if err != nil {
			return err
		}
		if count < diffScanBatchSize {
			return nil
		}
		afterMachine, afterOrdinal = machine, ordinal
		if err := yieldDiffScan(ctx); err != nil {
			return err
		}
	}
}

//nolint:funlen // One row contains paired before/after ROM projections and is decoded atomically.
func (service *Service) scanROMDiffBatch(
	ctx context.Context,
	baseID, targetID, afterMachine string,
	afterOrdinal int64,
	visit func(DiffItem),
) (int, string, int64, error) {
	rows, err := service.database.QueryContext(ctx, `
WITH b AS (
SELECT * FROM dat_rom_entries
WHERE dat_version_id=?
AND (machine_name,ordinal)>(?,?)
ORDER BY machine_name COLLATE BINARY,ordinal
LIMIT ?
), t AS (
SELECT * FROM dat_rom_entries
WHERE dat_version_id=?
AND (machine_name,ordinal)>(?,?)
ORDER BY machine_name COLLATE BINARY,ordinal
LIMIT ?
) SELECT COALESCE(b.machine_name,t.machine_name),
COALESCE(b.ordinal,t.ordinal),
b.machine_name,
b.name,
b.size_bytes,
b.crc32,
b.sha1,
b.status,
b.merge_name,
b.bios_name,
t.machine_name,
t.name,
t.size_bytes,
t.crc32,
t.sha1,
t.status,
t.merge_name,
t.bios_name
FROM b
FULL OUTER JOIN t ON t.machine_name=b.machine_name
AND t.ordinal=b.ordinal
ORDER BY COALESCE(b.machine_name,t.machine_name) COLLATE BINARY,
COALESCE(b.ordinal,t.ordinal)
LIMIT ?
`,
		baseID, afterMachine, afterOrdinal, diffScanBatchSize,
		targetID, afterMachine, afterOrdinal, diffScanBatchSize,
		diffScanBatchSize,
	)
	if err != nil {
		return 0, "", 0, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	count := 0
	lastMachine := afterMachine
	lastOrdinal := afterOrdinal
	for rows.Next() {
		var machine string
		var ordinal int64
		var bExists, bName, bCRC, bSHA, bStatus, bMerge, bBIOS sql.NullString
		var tExists, tName, tCRC, tSHA, tStatus, tMerge, tBIOS sql.NullString
		var bSize, tSize sql.NullInt64
		if err := rows.Scan(
			&machine,
			&ordinal,
			&bExists,
			&bName,
			&bSize,
			&bCRC,
			&bSHA,
			&bStatus,
			&bMerge,
			&bBIOS,
			&tExists,
			&tName,
			&tSize,
			&tCRC,
			&tSHA,
			&tStatus,
			&tMerge,
			&tBIOS,
		); err != nil {
			return 0, "", 0, fmt.Errorf("arcadecatalog/service: %w", err)
		}
		count++
		lastMachine, lastOrdinal = machine, ordinal
		before := romObject(bExists, bName, bSize, bCRC, bSHA, bStatus, bMerge, bBIOS)
		after := romObject(tExists, tName, tSize, tCRC, tSHA, tStatus, tMerge, tBIOS)
		change := classifyDiff(before, after)
		if change == "" {
			continue
		}
		name := bName.String
		if tName.Valid {
			name = tName.String
		}
		cursorKey := machine + "\x00" + fmt.Sprintf("%020d", ordinal) + "\x00" + name
		visit(
			DiffItem{
				Section: "ROM_ENTRIES",
				Change:  change,
				Key:     map[string]any{"machineName": machine, "ordinal": ordinal, "name": name},
				Before:  before,
				After:   after,
				cursor:  cursorKey,
			},
		)
	}
	if err := rows.Err(); err != nil {
		return 0, "", 0, fmt.Errorf("scan ROM diff: %w", err)
	}
	return count, lastMachine, lastOrdinal, nil
}

func romObject(
	exists, name sql.NullString,
	size sql.NullInt64,
	crc, sha, status, merge, bios sql.NullString,
) map[string]any {
	if !exists.Valid {
		return nil
	}
	return map[string]any{
		"name":      name.String,
		"sizeBytes": nullableInteger(size),
		"crc32":     nullableValue(crc),
		"sha1":      nullableValue(sha),
		"status":    nullableValue(status),
		"mergeName": nullableValue(merge),
		"biosName":  nullableValue(bios),
	}
}

func (service *Service) scanBIOSDiff(ctx context.Context, baseID, targetID string, visit func(DiffItem)) error {
	afterMachine := ""
	afterBIOS := ""
	for {
		count, machine, bios, err := service.scanBIOSDiffBatch(
			ctx,
			baseID,
			targetID,
			afterMachine,
			afterBIOS,
			visit,
		)
		if err != nil {
			return err
		}
		if count < diffScanBatchSize {
			return nil
		}
		afterMachine, afterBIOS = machine, bios
		if err := yieldDiffScan(ctx); err != nil {
			return err
		}
	}
}

//nolint:funlen // One row contains paired before/after BIOS projections and is decoded atomically.
func (service *Service) scanBIOSDiffBatch(
	ctx context.Context,
	baseID, targetID, afterMachine, afterBIOS string,
	visit func(DiffItem),
) (int, string, string, error) {
	rows, err := service.database.QueryContext(ctx, `
WITH b AS (
SELECT * FROM dat_bios_sets
WHERE dat_version_id=?
AND (machine_name,bios_name)>(?,?)
ORDER BY machine_name COLLATE BINARY,bios_name COLLATE BINARY
LIMIT ?
), t AS (
SELECT * FROM dat_bios_sets
WHERE dat_version_id=?
AND (machine_name,bios_name)>(?,?)
ORDER BY machine_name COLLATE BINARY,bios_name COLLATE BINARY
LIMIT ?
) SELECT COALESCE(b.machine_name,t.machine_name),
COALESCE(b.bios_name,t.bios_name),
b.machine_name,
b.description,
b.is_default,
t.machine_name,
t.description,
t.is_default
FROM b
FULL OUTER JOIN t ON t.machine_name=b.machine_name
AND t.bios_name=b.bios_name
ORDER BY COALESCE(b.machine_name,t.machine_name) COLLATE BINARY,
COALESCE(b.bios_name,t.bios_name) COLLATE BINARY
LIMIT ?
`,
		baseID, afterMachine, afterBIOS, diffScanBatchSize,
		targetID, afterMachine, afterBIOS, diffScanBatchSize,
		diffScanBatchSize,
	)
	if err != nil {
		return 0, "", "", fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	count := 0
	lastMachine := afterMachine
	lastBIOS := afterBIOS
	for rows.Next() {
		var machine, bios string
		var bExists, bDescription, tExists, tDescription sql.NullString
		var bDefault, tDefault sql.NullInt64
		if err := rows.Scan(
			&machine,
			&bios,
			&bExists,
			&bDescription,
			&bDefault,
			&tExists,
			&tDescription,
			&tDefault,
		); err != nil {
			return 0, "", "", fmt.Errorf("arcadecatalog/service: %w", err)
		}
		count++
		lastMachine, lastBIOS = machine, bios
		before := biosObject(bExists, bDescription, bDefault)
		after := biosObject(tExists, tDescription, tDefault)
		change := classifyDiff(before, after)
		if change == "" {
			continue
		}
		visit(
			DiffItem{
				Section: "BIOS_SETS",
				Change:  change,
				Key:     map[string]any{"machineName": machine, "biosName": bios},
				Before:  before,
				After:   after,
				cursor:  machine + "\x00" + bios,
			},
		)
	}
	if err := rows.Err(); err != nil {
		return 0, "", "", fmt.Errorf("scan BIOS diff: %w", err)
	}
	return count, lastMachine, lastBIOS, nil
}

func biosObject(exists, description sql.NullString, isDefault sql.NullInt64) map[string]any {
	if !exists.Valid {
		return nil
	}
	return map[string]any{"description": description.String, "isDefault": isDefault.Int64 == 1}
}

type ActivateRequest struct {
	ImpactDigest                string `json:"impactDigest"`
	ConfirmBlocked              bool   `json:"confirmBlocked"`
	ConfirmUnknownCompatibility bool   `json:"confirmUnknownCompatibility"`
}

type Activated struct {
	DATVersionID  string `json:"datVersionId"`
	Active        bool   `json:"active"`
	Version       int64  `json:"version"`
	ActivatedAtMS int64  `json:"activatedAtMs"`
}

//nolint:funlen,gocyclo // Activation branches are independent invariant checks inside one optimistic-lock transaction.
func (service *Service) Activate(
	ctx context.Context,
	datID string,
	artifactVersion int64,
	request ActivateRequest,
	rollback bool,
) (Activated, error) {
	diff, err := service.Diff(ctx, datID)
	if err != nil || subtle.ConstantTimeCompare([]byte(diff.ImpactDigest), []byte(request.ImpactDigest)) != 1 {
		return Activated{}, ErrInvalid
	}
	if blocked := numericInt64(diff.Impact["blockedVariantCount"]); blocked > 0 && !request.ConfirmBlocked {
		return Activated{}, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Activated{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var artifactID, compatibility, parseStatus string
	var datVersion int64
	var activatedAt sql.NullInt64
	if err := transaction.QueryRowContext(ctx, `
SELECT core_artifact_id,
compatibility_status,
parse_status,
version,
activated_at_ms
FROM dat_versions
WHERE id=?
`, datID).Scan(&artifactID, &compatibility, &parseStatus, &datVersion, &activatedAt); err != nil ||
		parseStatus != "READY" ||
		compatibility == "INCOMPATIBLE" ||
		rollback != activatedAt.Valid {
		return Activated{}, ErrInvalid
	}
	var currentArtifactVersion int64
	if err := transaction.QueryRowContext(ctx, `
SELECT version
FROM core_artifacts
WHERE id=?
AND enabled=1
`, artifactID).Scan(&currentArtifactVersion); err != nil ||
		currentArtifactVersion != artifactVersion {
		return Activated{}, ErrInvalid
	}
	if compatibility == "UNKNOWN" && !request.ConfirmUnknownCompatibility {
		return Activated{}, ErrInvalid
	}
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0,
version=version+1,
updated_at_ms=?
WHERE core_artifact_id=?
AND is_active=1
`, now, artifactID); err != nil {
		return Activated{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=1,
compatibility_status=CASE WHEN compatibility_status='UNKNOWN' THEN 'USER_CONFIRMED' ELSE compatibility_status END,
version=version+1,
updated_at_ms=?,
activated_at_ms=?
WHERE id=?
`, now, now, datID); err != nil {
		return Activated{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if err := datindex.SyncRequirements(ctx, transaction, datID, service.now()); err != nil {
		return Activated{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if service.hooks.Queue != nil {
		if _, err := service.hooks.Queue(ctx, transaction, artifactID, datID); err != nil {
			return Activated{}, err
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts
SET version=version+1,
updated_at_ms=?
WHERE id=?
AND version=?
	`, now, artifactID, artifactVersion); err != nil {
		return Activated{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM dat_diff_items
WHERE snapshot_id IN (SELECT s.id
FROM dat_diff_snapshots s
JOIN dat_versions d ON d.id=s.dat_version_id
WHERE d.core_artifact_id=?)
`, artifactID); err != nil {
		return Activated{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_diff_snapshots
SET state='STALE',
summary_json=NULL,
impact_json=NULL,
impact_digest=NULL,
error_code=NULL,
completed_at_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE dat_version_id IN (SELECT id
FROM dat_versions
WHERE core_artifact_id=?)
`, now, artifactID); err != nil {
		return Activated{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Activated{}, fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if service.hooks.Resume != nil {
		service.hooks.Resume()
	}
	return Activated{DATVersionID: datID, Active: true, Version: datVersion + 1, ActivatedAtMS: now}, nil
}

func numericInt64(value any) int64 {
	switch item := value.(type) {
	case int64:
		return item
	case float64:
		return int64(item)
	case json.Number:
		result, _ := item.Int64()
		return result
	default:
		return 0
	}
}

func (service *Service) Delete(ctx context.Context, datID string, expectedVersion int64) error {
	result, err := service.database.ExecContext(
		ctx,
		`
DELETE
FROM dat_versions
WHERE id=?
AND source='USER'
AND is_active=0
AND activated_at_ms IS NULL
AND version=?
AND NOT EXISTS(SELECT 1
FROM game_variant_revisions
WHERE dat_version_id=?)
`,
		datID,
		expectedVersion,
		datID,
	)
	if err != nil {
		return fmt.Errorf("arcadecatalog/service: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrInvalid
	}
	return nil
}

func MarshalDiff(value Diff) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
