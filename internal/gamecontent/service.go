package gamecontent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/contentmanifest"
)

var (
	ErrInvalid              = errors.New("GAME_CONTENT_INVALID")
	ErrIdempotencyKeyReused = errors.New("IDEMPOTENCY_KEY_REUSED")
)

type Scheduled struct {
	GameID  string `json:"gameId"`
	JobID   string `json:"jobId"`
	State   string `json:"state"`
	Version int64  `json:"version"`
}

type Service struct {
	database *sql.DB
	now      func() time.Time
}

type jobSnapshot struct {
	ExecutionID             string  `json:"executionId"`
	GameID                  string  `json:"gameId"`
	GameVersion             int64   `json:"gameVersion"`
	BaseContentRevisionID   string  `json:"baseContentRevisionId"`
	UploadSessionID         string  `json:"uploadSessionId"`
	PlatformID              string  `json:"platformId"`
	PlatformInstanceID      string  `json:"platformInstanceId"`
	PlatformInstanceVersion int64   `json:"platformInstanceVersion"`
	CoreID                  string  `json:"coreId"`
	CoreArtifactID          string  `json:"coreArtifactId"`
	DATVersionID            *string `json:"datVersionId"`
	ConfigSnapshotDigest    string  `json:"configSnapshotDigest"`
}

type uploadedFile struct {
	logicalName, blobID, sha256 string
	sizeBytes                   int64
}

type inputEnvelope struct {
	SchemaVersion int            `json:"schemaVersion"`
	Kind          string         `json:"kind"`
	Scope         map[string]any `json:"scope"`
	ExecutionID   string         `json:"executionId"`
	Inputs        jobSnapshot    `json:"inputs"`
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

func (service *Service) Schedule(
	ctx context.Context,
	gameID, uploadID string,
	expectedVersion int64,
) (Scheduled, error) {
	result, _, err := service.schedule(ctx, gameID, uploadID, expectedVersion, "", "")
	return result, err
}

func (service *Service) ScheduleIdempotent(
	ctx context.Context,
	gameID, uploadID string,
	expectedVersion int64,
	key, requestDigest string,
) (Scheduled, bool, error) {
	if key == "" || len(requestDigest) != 64 {
		return Scheduled{}, false, ErrInvalid
	}
	return service.schedule(ctx, gameID, uploadID, expectedVersion, key, requestDigest)
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) schedule(
	ctx context.Context,
	gameID, uploadID string,
	expectedVersion int64,
	key, requestDigest string,
) (Scheduled, bool, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Scheduled{}, false, fmt.Errorf("gamecontent/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	if key != "" {
		if _, err := transaction.ExecContext(ctx, `
DELETE
FROM idempotency_records
WHERE operation_id='postAdminGameContentRevision'
AND key=?
AND expires_at_ms<=?
`, key, now); err != nil {
			return Scheduled{}, false, fmt.Errorf("gamecontent/service: %w", err)
		}
		var storedDigest string
		var storedBody []byte
		err := transaction.QueryRowContext(ctx, `
SELECT request_digest,
response_body
FROM idempotency_records
WHERE operation_id='postAdminGameContentRevision'
AND key=?
`, key).
			Scan(&storedDigest, &storedBody)
		if err == nil {
			if storedDigest != requestDigest {
				return Scheduled{}, false, ErrIdempotencyKeyReused
			}
			var stored Scheduled
			if json.Unmarshal(storedBody, &stored) != nil {
				return Scheduled{}, false, ErrInvalid
			}
			return stored, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Scheduled{}, false, fmt.Errorf("gamecontent/service: %w", err)
		}
	}
	var contentID, instanceID, platformID, coreID, artifactID string
	var version, platformVersion int64
	var datID sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT g.current_content_revision_id,
g.platform_instance_id,
pi.platform_id,
pi.default_core_id,
a.id,
(SELECT id
FROM dat_versions
WHERE core_artifact_id=a.id
AND is_active=1),
g.version,
pi.version
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN core_artifacts a ON a.core_id=pi.default_core_id
AND a.enabled=1
WHERE g.id=?
AND g.status='PUBLISHED'
`, gameID).
		Scan(&contentID, &instanceID, &platformID, &coreID, &artifactID, &datID, &version, &platformVersion)
	if err != nil || version != expectedVersion {
		return Scheduled{}, false, ErrInvalid
	}
	var uploadState string
	var fileCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT state,
(SELECT count(*)
FROM upload_files
WHERE upload_session_id=upload_sessions.id
AND state='COMPLETE')
FROM upload_sessions
WHERE id=?
`, uploadID).Scan(&uploadState, &fileCount); err != nil ||
		uploadState != "COMPLETE" ||
		fileCount == 0 ||
		platformID != "dos" && fileCount != 1 {
		return Scheduled{}, false, ErrInvalid
	}
	var consumed int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM upload_consumptions
WHERE upload_session_id=?
`, uploadID).Scan(&consumed); err != nil ||
		consumed != 0 {
		return Scheduled{}, false, ErrInvalid
	}
	jobID, consumptionID, executionID := newID(), newID(), newID()
	configInput := fmt.Sprintf("%s\x00%d\x00%s\x00%s", instanceID, platformVersion, artifactID, nullableText(datID))
	configDigest := sha256.Sum256([]byte(configInput))
	snapshot := jobSnapshot{
		ExecutionID:             executionID,
		GameID:                  gameID,
		GameVersion:             expectedVersion,
		BaseContentRevisionID:   contentID,
		UploadSessionID:         uploadID,
		PlatformID:              platformID,
		PlatformInstanceID:      instanceID,
		PlatformInstanceVersion: platformVersion,
		CoreID:                  coreID,
		CoreArtifactID:          artifactID,
		DATVersionID:            nullablePointer(datID),
		ConfigSnapshotDigest:    hex.EncodeToString(configDigest[:]),
	}
	envelope := inputEnvelope{
		SchemaVersion: 1,
		Kind:          "GAME_FILE_REVISION",
		Scope:         map[string]any{"type": "GAME", "id": gameID},
		ExecutionID:   executionID,
		Inputs:        snapshot,
	}
	inputJSON, _ := json.Marshal(envelope)
	dedupeInput, _ := json.Marshal(map[string]any{"executionId": executionID, "gameId": gameID})
	dedupe := sha256.Sum256(append([]byte("retrom-job-dedupe-v1\x00GAME_FILE_REVISION\x00"), dedupeInput...))
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
'GAME_FILE_REVISION',
?,
1,
'{"schemaVersion":1,"inputExecutionNo":1}',
1,
'QUEUED',
0,
2,
?,
?,
?)
`, jobID, gameID, hex.EncodeToString(dedupe[:]), now, now, now); err != nil {
		return Scheduled{}, false, ErrInvalid
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,
upload_session_id,
upload_file_id,
consumer_type,
consumer_id,
created_at_ms) VALUES(?,
?,
NULL,
'GAME_FILE_REVISION_JOB',
?,
?)
`, consumptionID, uploadID, jobID, now); err != nil {
		return Scheduled{}, false, ErrInvalid
	}
	inputDigest := sha256.Sum256(inputJSON)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,
execution_no,
input_json,
input_digest,
created_at_ms) VALUES(?,
1,
?,
?,
?)
`, jobID, string(inputJSON), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return Scheduled{}, false, fmt.Errorf("gamecontent/service: %w", err)
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
		return Scheduled{}, false, fmt.Errorf("gamecontent/service: %w", err)
	}
	result := Scheduled{GameID: gameID, JobID: jobID, State: "QUEUED", Version: expectedVersion}
	if key != "" {
		responseBody, _ := json.Marshal(result)
		headers, _ := json.Marshal(
			map[string]string{
				"Content-Type": "application/json; charset=utf-8",
				"ETag":         fmt.Sprintf(`"v%d"`, expectedVersion),
			},
		)
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO idempotency_records(operation_id,
key,
request_digest,
http_status,
response_headers_json,
response_body,
created_at_ms,
expires_at_ms) VALUES('postAdminGameContentRevision',
?,
?,
202,
?,
?,
?,
?)
`, key, requestDigest, string(headers), responseBody, now, now+int64(24*time.Hour/time.Millisecond)); err != nil {
			return Scheduled{}, false, fmt.Errorf("gamecontent/service: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return Scheduled{}, false, fmt.Errorf("gamecontent/service: %w", err)
	}
	go service.run(context.WithoutCancel(ctx), jobID, snapshot)
	return result, false, nil
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
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
worker_id='local',
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
	gameID := snapshot.GameID
	uploadID := snapshot.UploadSessionID
	platformID := snapshot.PlatformID
	coreID := snapshot.CoreID
	artifactID := snapshot.CoreArtifactID
	previousContentID := snapshot.BaseContentRevisionID
	files, err := collectUploadFiles(ctx, service.database, uploadID)
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_INPUT_UNAVAILABLE")
		return
	}
	if len(files) == 0 || platformID != "dos" && len(files) != 1 ||
		platformID == "arcade" && !strings.EqualFold(filepath.Ext(files[0].logicalName), ".zip") {
		service.fail(ctx, jobID, "GAME_CONTENT_GROUP_INVALID")
		return
	}
	manifestFiles := make([]contentmanifest.File, 0, len(files))
	for index, value := range files {
		role := "COMPANION"
		if index == 0 {
			role = "CONTENT"
		}
		manifestFiles = append(
			manifestFiles,
			contentmanifest.File{
				Role:        role,
				LogicalName: value.logicalName,
				BlobSHA256:  value.sha256,
				SizeBytes:   value.sizeBytes,
			},
		)
	}
	manifest, manifestDigestHex, err := contentmanifest.Build(manifestFiles)
	if err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_MANIFEST_INVALID")
		return
	}
	manifestDigest := sha256.Sum256(manifest)
	validationInput := sha256.Sum256(
		append(append([]byte{}, manifestDigest[:]...), []byte(artifactID+fmt.Sprint(snapshot.DATVersionID))...),
	)
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
	var currentInstance, currentArtifact string
	var currentPlatformVersion int64
	var currentDAT sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT g.current_content_revision_id,
g.version,
g.platform_instance_id,
pi.version,
a.id,
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
		&currentDAT,
	); err != nil ||
		currentContent != previousContentID ||
		currentVersion != snapshot.GameVersion ||
		currentInstance != snapshot.PlatformInstanceID ||
		currentPlatformVersion != snapshot.PlatformInstanceVersion ||
		currentArtifact != snapshot.CoreArtifactID ||
		nullableText(currentDAT) != pointerText(snapshot.DATVersionID) {
		cleanup.Rollback(transaction)
		service.fail(ctx, jobID, "GAME_CONTENT_SNAPSHOT_STALE")
		return
	}
	contentID := newID()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_content_revisions(id,
game_id,
source_kind,
source_ref_id,
source_manifest_json,
source_manifest_digest,
created_at_ms) VALUES(?,
?,
'ADMIN_REPLACE',
?,
?,
?,
?)
`, contentID, gameID, jobID, string(manifest), manifestDigestHex, now); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	for index, value := range files {
		role := "COMPANION"
		if index == 0 {
			role = "CONTENT"
		}
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
`, contentID, role, value.logicalName, value.blobID, index); err != nil {
			failTransaction("GAME_CONTENT_DATABASE_FAILED")
			return
		}
	}
	var variantID string
	err = transaction.QueryRowContext(ctx, `
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
			failTransaction("GAME_CONTENT_DATABASE_FAILED")
			return
		}
	} else if err != nil {
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
		hex.EncodeToString(validationInput[:]),
		emulatorGameID,
		`{"schemaVersion":1}`,
		now,
	); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
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
AND worker_id='local'
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
		fmt.Sprintf(`{"contentRevisionId":%q,"variantRevisionId":%q}`, contentID, revisionID),
		finished,
	); err != nil {
		failTransaction("GAME_CONTENT_DATABASE_FAILED")
		return
	}
	if err := transaction.Commit(); err != nil {
		service.fail(ctx, jobID, "GAME_CONTENT_DATABASE_FAILED")
	}
}

func collectUploadFiles(ctx context.Context, database *sql.DB, uploadID string) ([]uploadedFile, error) {
	rows, err := database.QueryContext(
		ctx,
		`
SELECT f.relative_path,
f.final_blob_id,
b.sha256,
b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.upload_session_id=?
AND f.state='COMPLETE'
ORDER BY f.relative_path,
f.id
`,
		uploadID,
	)
	if err != nil {
		return nil, fmt.Errorf("query upload files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]uploadedFile, 0)
	for rows.Next() {
		var value uploadedFile
		if err := rows.Scan(&value.logicalName, &value.blobID, &value.sha256, &value.sizeBytes); err != nil {
			return nil, fmt.Errorf("scan upload file: %w", err)
		}
		files = append(files, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upload files: %w", err)
	}
	return files, nil
}

func (service *Service) fail(ctx context.Context, jobID, code string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(
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
	)
	_, _ = service.database.ExecContext(
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
	)
}

func nullablePointer(value sql.NullString) *string {
	if value.Valid {
		return &value.String
	}
	return nil
}

func nullableText(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func newID() string {
	value, _ := uuid.NewV7()
	return value.String()
}
