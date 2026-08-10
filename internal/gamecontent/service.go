package gamecontent

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
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentmanifest"
	"retrom/internal/contentprofile"
	"retrom/internal/corevalidation"
	"retrom/internal/multidisc"
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
	database               *sql.DB
	blobs                  *blobstore.Store
	multiDiscImportEnabled bool
	now                    func() time.Time
}

type jobSnapshot struct {
	ExecutionID               string  `json:"executionId"`
	GameID                    string  `json:"gameId"`
	GameVersion               int64   `json:"gameVersion"`
	BaseContentRevisionID     string  `json:"baseContentRevisionId"`
	UploadSessionID           string  `json:"uploadSessionId"`
	PlatformID                string  `json:"platformId"`
	PlatformInstanceID        string  `json:"platformInstanceId"`
	PlatformInstanceVersion   int64   `json:"platformInstanceVersion"`
	CoreID                    string  `json:"coreId"`
	CoreArtifactID            string  `json:"coreArtifactId"`
	CoreArtifactVersion       int64   `json:"coreArtifactVersion"`
	CompatibilityConfigDigest string  `json:"compatibilityConfigDigest"`
	ContentMode               string  `json:"contentMode"`
	MaxDiscs                  int     `json:"maxDiscs,omitempty"`
	MaxTotalBytes             int64   `json:"maxTotalBytes,omitempty"`
	DATVersionID              *string `json:"datVersionId"`
	ConfigSnapshotDigest      string  `json:"configSnapshotDigest"`
}

type uploadedFile struct {
	logicalName, blobID, sha256 string
	sizeBytes                   int64
}

type replacementFile struct {
	role, logicalName, blobID, sha256 string
	sizeBytes                         int64
	sortOrder                         int
}

type preparedReplacement struct {
	contentKind             string
	files                   []replacementFile
	manifest                []byte
	manifestDigest          string
	canonicalPlaylist       blobstore.Metadata
	orderedDiscSHA256       []string
	firstContentLogicalName string
}

type replacementValidationError struct{ code string }

func (err *replacementValidationError) Error() string { return err.code }

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

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func (service *Service) WithMultiDiscImportEnabled(enabled bool) *Service {
	service.multiDiscImportEnabled = enabled
	return service
}

func (service *Service) Schedule(
	ctx context.Context,
	gameID, uploadID string,
	expectedVersion int64,
) (Scheduled, error) {
	result, _, err := service.schedule(
		ctx, gameID, uploadID, expectedVersion, contentcapability.ModeStandard, "", "",
	)
	return result, err
}

func (service *Service) ScheduleMode(
	ctx context.Context,
	gameID, uploadID, contentMode string,
	expectedVersion int64,
) (Scheduled, error) {
	result, _, err := service.schedule(ctx, gameID, uploadID, expectedVersion, contentMode, "", "")
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
	return service.schedule(
		ctx, gameID, uploadID, expectedVersion, contentcapability.ModeStandard, key, requestDigest,
	)
}

func (service *Service) ScheduleIdempotentMode(
	ctx context.Context,
	gameID, uploadID, contentMode string,
	expectedVersion int64,
	key, requestDigest string,
) (Scheduled, bool, error) {
	if key == "" || len(requestDigest) != 64 {
		return Scheduled{}, false, ErrInvalid
	}
	return service.schedule(ctx, gameID, uploadID, expectedVersion, contentMode, key, requestDigest)
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) schedule(
	ctx context.Context,
	gameID, uploadID string,
	expectedVersion int64,
	contentMode string,
	key, requestDigest string,
) (Scheduled, bool, error) {
	if contentMode == "" {
		contentMode = contentcapability.ModeStandard
	}
	if contentMode != contentcapability.ModeStandard && contentMode != contentcapability.ModeMultiDiscM3UV1 {
		return Scheduled{}, false, ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Scheduled{}, false, fmt.Errorf("gamecontent/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	now := service.now().UnixMilli()
	principal, _ := authn.PrincipalFromContext(ctx)
	principalID := principal.UserID
	if principalID == "" {
		principalID = "SYSTEM"
	}
	if key != "" {
		if _, err := transaction.ExecContext(ctx, `
DELETE
FROM idempotency_records
WHERE operation_id='postAdminGameContentRevision'
AND key=?
AND principal_id=?
AND expires_at_ms<=?
`, key, principalID, now); err != nil {
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
AND principal_id=?
`, key, principalID).
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
	var contentID, instanceID, platformID, coreID, artifactID, compatibilityJSON string
	var version, platformVersion, artifactVersion int64
	var datID sql.NullString
	err = transaction.QueryRowContext(ctx, `
SELECT g.current_content_revision_id,
g.platform_instance_id,
pi.platform_id,
pi.default_core_id,
a.id,
a.version,
a.compatibility_config_json,
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
		Scan(
			&contentID, &instanceID, &platformID, &coreID, &artifactID, &artifactVersion,
			&compatibilityJSON, &datID, &version, &platformVersion,
		)
	if err != nil || version != expectedVersion {
		return Scheduled{}, false, ErrInvalid
	}
	capabilities := contentcapability.Resolve(
		platformID, true, service.multiDiscImportEnabled, compatibilityJSON,
	)
	if contentMode == contentcapability.ModeMultiDiscM3UV1 && capabilities.MultiDisc == nil {
		return Scheduled{}, false, ErrInvalid
	}
	var uploadState, sourceType string
	var fileCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT state,source_type,
(SELECT count(*)
FROM upload_files
WHERE upload_session_id=upload_sessions.id
AND state='COMPLETE')
FROM upload_sessions
WHERE id=?
`, uploadID).Scan(&uploadState, &sourceType, &fileCount); err != nil ||
		uploadState != "COMPLETE" ||
		fileCount == 0 ||
		contentMode == contentcapability.ModeStandard && platformID != "dos" && fileCount != 1 ||
		contentMode == contentcapability.ModeMultiDiscM3UV1 && sourceType != "DIRECTORY" {
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
	compatibilityDigest := corevalidation.CompatibilityConfigDigest(compatibilityJSON)
	configInput := fmt.Sprintf(
		"%s\x00%d\x00%s\x00%d\x00%s\x00%s\x00%s",
		instanceID, platformVersion, artifactID, artifactVersion, compatibilityDigest,
		contentMode, nullableText(datID),
	)
	configDigest := sha256.Sum256([]byte(configInput))
	snapshot := jobSnapshot{
		ExecutionID:               executionID,
		GameID:                    gameID,
		GameVersion:               expectedVersion,
		BaseContentRevisionID:     contentID,
		UploadSessionID:           uploadID,
		PlatformID:                platformID,
		PlatformInstanceID:        instanceID,
		PlatformInstanceVersion:   platformVersion,
		CoreID:                    coreID,
		CoreArtifactID:            artifactID,
		CoreArtifactVersion:       artifactVersion,
		CompatibilityConfigDigest: compatibilityDigest,
		ContentMode:               contentMode,
		DATVersionID:              nullablePointer(datID),
		ConfigSnapshotDigest:      hex.EncodeToString(configDigest[:]),
	}
	if capabilities.MultiDisc != nil && contentMode == contentcapability.ModeMultiDiscM3UV1 {
		snapshot.MaxDiscs = capabilities.MultiDisc.MaxDiscs
		snapshot.MaxTotalBytes = capabilities.MultiDisc.MaxTotalBytes
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
INSERT INTO idempotency_records(principal_id,
operation_id,
key,
request_digest,
http_status,
response_headers_json,
response_body,
created_at_ms,
expires_at_ms) VALUES(?,
'postAdminGameContentRevision',
?,
?,
202,
?,
?,
?,
?)
`,
			principalID,
			key,
			requestDigest,
			string(headers),
			responseBody,
			now,
			now+int64(24*time.Hour/time.Millisecond),
		); err != nil {
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
	gameID := snapshot.GameID
	uploadID := snapshot.UploadSessionID
	coreID := snapshot.CoreID
	artifactID := snapshot.CoreArtifactID
	previousContentID := snapshot.BaseContentRevisionID
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
	for _, value := range prepared.files {
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
	validationInputDigest := ""
	if prepared.contentKind == multidisc.ContentKind {
		biosDigest, digestErr := corevalidation.BIOSDependencyDigest(biosSnapshot)
		if digestErr != nil {
			failTransaction("GAME_CONTENT_DATABASE_FAILED")
			return
		}
		validationInputDigest, err = corevalidation.MultiDiscValidationInputDigest(
			corevalidation.MultiDiscValidationInput{
				GameVariantID: variantID, GameContentRevisionID: contentID,
				ContentKind: prepared.contentKind, CoreArtifactID: artifactID,
				CoreArtifactVersion:       snapshot.CoreArtifactVersion,
				CompatibilityConfigSHA256: snapshot.CompatibilityConfigDigest,
				DATVersionID:              currentDAT, BIOSDependencySHA256: biosDigest,
				OrderedDiscSHA256:       prepared.orderedDiscSHA256,
				CanonicalPlaylistSHA256: prepared.canonicalPlaylist.SHA256,
			},
		)
	} else {
		validationInputDigest, err = corevalidation.ValidationInputDigest(
			artifactID, contentID, currentDAT, biosSnapshot,
		)
	}
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
	if prepared.contentKind == multidisc.ContentKind {
		playlistBlobID, ensureErr := blobstore.EnsureRecord(
			ctx, transaction, prepared.canonicalPlaylist, "application/vnd.retrom.m3u", now,
		)
		if ensureErr != nil {
			failTransaction("GAME_CONTENT_DATABASE_FAILED")
			return
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_revision_id,role,logical_name,blob_id,sort_order)
VALUES(?,'MULTI_DISC_PLAYLIST','playlist.m3u',?,0)
`, revisionID, playlistBlobID); err != nil {
			failTransaction("GAME_CONTENT_DATABASE_FAILED")
			return
		}
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

func (service *Service) prepareReplacement(
	ctx context.Context,
	snapshot jobSnapshot,
	files []uploadedFile,
) (preparedReplacement, error) {
	if snapshot.ContentMode != contentcapability.ModeMultiDiscM3UV1 {
		if len(files) == 0 || snapshot.PlatformID != "dos" && len(files) != 1 ||
			snapshot.PlatformID == "arcade" && !strings.EqualFold(filepath.Ext(files[0].logicalName), ".zip") {
			return preparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_GROUP_INVALID"}
		}
		replacement := preparedReplacement{contentKind: string(contentprofile.ContentKindSingleFile)}
		replacement.files = make([]replacementFile, 0, len(files))
		manifestFiles := make([]contentmanifest.File, 0, len(files))
		for index, file := range files {
			role := "COMPANION"
			if index == 0 {
				role = "CONTENT"
			}
			replacement.files = append(replacement.files, replacementFile{
				role: role, logicalName: file.logicalName, blobID: file.blobID,
				sha256: file.sha256, sizeBytes: file.sizeBytes, sortOrder: index,
			})
			manifestFiles = append(manifestFiles, contentmanifest.File{
				Role: role, LogicalName: file.logicalName, BlobSHA256: file.sha256, SizeBytes: file.sizeBytes,
			})
		}
		replacement.firstContentLogicalName = files[0].logicalName
		manifest, digest, err := contentmanifest.Build(manifestFiles)
		if err != nil {
			return preparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
		}
		replacement.manifest, replacement.manifestDigest = manifest, digest
		return replacement, nil
	}
	return service.prepareMultiDiscReplacement(ctx, snapshot, files)
}

func (service *Service) prepareMultiDiscReplacement(
	ctx context.Context,
	snapshot jobSnapshot,
	files []uploadedFile,
) (preparedReplacement, error) {
	if err := ctx.Err(); err != nil {
		return preparedReplacement{}, fmt.Errorf("prepare multi-disc replacement: %w", err)
	}
	if service.blobs == nil || snapshot.MaxDiscs < multidisc.MinDiscs || snapshot.MaxDiscs > multidisc.MaxDiscs ||
		snapshot.MaxTotalBytes <= 0 {
		return preparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_VALIDATION_UNAVAILABLE"}
	}
	playlist, err := replacementPlaylist(files)
	if err != nil {
		return preparedReplacement{}, err
	}
	playlistBytes, err := service.readReplacementPlaylist(playlist)
	if err != nil {
		return preparedReplacement{}, err
	}
	directory := path.Dir(playlist.logicalName)
	candidates, err := service.replacementDiscCandidates(files, playlist.blobID, directory)
	if err != nil {
		return preparedReplacement{}, err
	}
	parsed, err := multidisc.Parse(playlistBytes, candidates, multidisc.Limits{
		MaxDiscs: snapshot.MaxDiscs, MaxTotalBytes: snapshot.MaxTotalBytes,
	})
	if err != nil {
		var validationErr *multidisc.ValidationError
		if errors.As(err, &validationErr) {
			return preparedReplacement{}, &replacementValidationError{code: string(validationErr.Code)}
		}
		return preparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_INVALID"}
	}
	for _, entry := range parsed.Entries {
		if entry.State != multidisc.EntryPresent || entry.File == nil {
			return preparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_FILE_MISSING"}
		}
	}
	canonical, err := service.blobs.Put(bytes.NewReader(parsed.CanonicalPlaylist))
	if err != nil {
		return preparedReplacement{}, &replacementValidationError{code: "MULTI_DISC_VALIDATION_UNAVAILABLE"}
	}
	return buildPreparedMultiDiscReplacement(playlist, parsed, canonical)
}

func replacementPlaylist(files []uploadedFile) (uploadedFile, error) {
	playlists := make([]uploadedFile, 0, 2)
	for _, file := range files {
		if strings.EqualFold(path.Ext(file.logicalName), ".m3u") {
			playlists = append(playlists, file)
		}
	}
	if len(playlists) == 0 {
		return uploadedFile{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_MISSING"}
	}
	if len(playlists) != 1 {
		return uploadedFile{}, &replacementValidationError{code: "MULTI_DISC_PLAYLIST_AMBIGUOUS"}
	}
	return playlists[0], nil
}

func (service *Service) readReplacementPlaylist(playlist uploadedFile) ([]byte, error) {
	playlistFile, err := service.blobs.OpenDigest(playlist.sha256)
	if err != nil {
		return nil, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	defer func() { cleanup.Error("close", playlistFile.Close()) }()
	playlistBytes, err := io.ReadAll(io.LimitReader(playlistFile, multidisc.MaxPlaylistBytes+1))
	if err != nil {
		return nil, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	return playlistBytes, nil
}

func (service *Service) replacementDiscCandidates(
	files []uploadedFile,
	playlistBlobID, directory string,
) ([]multidisc.File, error) {
	candidates := make([]multidisc.File, 0, len(files))
	for _, file := range files {
		if file.blobID == playlistBlobID || path.Dir(file.logicalName) != directory ||
			!strings.EqualFold(path.Ext(file.logicalName), ".chd") {
			continue
		}
		candidate, err := service.replacementDiscCandidate(file)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (service *Service) replacementDiscCandidate(file uploadedFile) (multidisc.File, error) {
	blob, err := service.blobs.OpenDigest(file.sha256)
	if err != nil {
		return multidisc.File{}, &replacementValidationError{code: "GAME_CONTENT_INPUT_UNAVAILABLE"}
	}
	defer func() { cleanup.Error("close", blob.Close()) }()
	header := make([]byte, 8)
	if _, err := io.ReadFull(blob, header); err != nil {
		return multidisc.File{}, &replacementValidationError{code: string(multidisc.CodeCHDInvalid)}
	}
	return multidisc.File{
		Basename: path.Base(file.logicalName), LogicalName: path.Base(file.logicalName),
		BlobID: file.blobID, BlobSHA256: file.sha256, SizeBytes: file.sizeBytes, Header: header,
	}, nil
}

func buildPreparedMultiDiscReplacement(
	playlist uploadedFile,
	parsed multidisc.Result,
	canonical blobstore.Metadata,
) (preparedReplacement, error) {
	replacement := preparedReplacement{
		contentKind: multidisc.ContentKind, canonicalPlaylist: canonical,
		files:                   make([]replacementFile, 0, len(parsed.Entries)+1),
		orderedDiscSHA256:       make([]string, 0, len(parsed.Entries)),
		firstContentLogicalName: parsed.Entries[0].File.LogicalName,
	}
	replacement.files = append(replacement.files, replacementFile{
		role: "PLAYLIST_SOURCE", logicalName: path.Base(playlist.logicalName), blobID: playlist.blobID,
		sha256: playlist.sha256, sizeBytes: playlist.sizeBytes, sortOrder: 0,
	})
	manifestFiles := make([]contentmanifest.File, 0, len(parsed.Entries)+1)
	manifestFiles = append(manifestFiles, contentmanifest.File{
		Role: "PLAYLIST_SOURCE", LogicalName: path.Base(playlist.logicalName),
		BlobSHA256: playlist.sha256, SizeBytes: playlist.sizeBytes,
	})
	for _, entry := range parsed.Entries {
		file := replacementFile{
			role: "DISC", logicalName: entry.File.LogicalName, blobID: entry.File.BlobID,
			sha256: entry.File.BlobSHA256, sizeBytes: entry.File.SizeBytes, sortOrder: entry.Ordinal,
		}
		replacement.files = append(replacement.files, file)
		replacement.orderedDiscSHA256 = append(replacement.orderedDiscSHA256, file.sha256)
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: file.role, LogicalName: file.logicalName, BlobSHA256: file.sha256, SizeBytes: file.sizeBytes,
		})
	}
	manifest, manifestDigest, err := contentmanifest.Build(manifestFiles)
	if err != nil {
		return preparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
	}
	replacement.manifest, replacement.manifestDigest = manifest, manifestDigest
	return replacement, nil
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
