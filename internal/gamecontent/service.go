package gamecontent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/corevalidation"
	"retrom/internal/payloadrelease"
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
	payloadReleases        *payloadrelease.Service
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
	CoreArtifactRouteKey      string  `json:"coreArtifactRouteKey"`
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

func (service *Service) WithPayloadRelease(releases *payloadrelease.Service) *Service {
	service.payloadReleases = releases
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

// Contract branches stay contiguous for a single auditable decision.
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
		replayed, replay, err := loadScheduledReplay(
			ctx, transaction, key, principalID, requestDigest, now,
		)
		if err != nil {
			return Scheduled{}, false, err
		}
		if replayed {
			return replay, true, nil
		}
	}
	result, err := service.scheduleFresh(
		ctx, transaction, gameID, uploadID, contentMode, key, requestDigest,
		principalID, expectedVersion, now,
	)
	return result, false, err
}

func (service *Service) scheduleFresh(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, uploadID, contentMode, key, requestDigest, principalID string,
	expectedVersion, now int64,
) (Scheduled, error) {
	var contentID, instanceID, platformID, coreID, artifactID, routeKey, compatibilityJSON string
	var version, platformVersion, artifactVersion int64
	var datID sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT g.current_content_revision_id,
g.platform_instance_id,
pi.platform_id,
pi.default_core_id,
a.id,
a.route_key,
a.version,
a.compatibility_json,
(SELECT id
FROM dat_versions
WHERE core_artifact_id=a.id
AND is_active=1),
g.version,
pi.version
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN core_artifacts a ON a.core_id=pi.default_core_id
AND a.selected_for_new_bindings=1 AND a.available_for_launch=1
WHERE g.id=?
AND g.status='PUBLISHED'
`, gameID).
		Scan(
			&contentID, &instanceID, &platformID, &coreID, &artifactID, &routeKey, &artifactVersion,
			&compatibilityJSON, &datID, &version, &platformVersion,
		)
	if err != nil || version != expectedVersion {
		return Scheduled{}, ErrInvalid
	}
	capabilities := contentcapability.Resolve(
		platformID, true, service.multiDiscImportEnabled, compatibilityJSON,
	)
	if contentMode == contentcapability.ModeMultiDiscM3UV1 && capabilities.MultiDisc == nil {
		return Scheduled{}, ErrInvalid
	}
	if err := validateReplacementUpload(ctx, transaction, uploadID, contentMode, platformID); err != nil {
		return Scheduled{}, err
	}
	jobID, consumptionID, executionID := newID(), newID(), newID()
	compatibilityDigest := corevalidation.CompatibilityConfigDigest(compatibilityJSON)
	configInput := fmt.Sprintf(
		"%s\x00%d\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s",
		instanceID, platformVersion, artifactID, routeKey, artifactVersion, compatibilityDigest,
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
		CoreArtifactRouteKey:      routeKey,
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
	return service.persistFreshSchedule(
		ctx, transaction, jobID, consumptionID, key, requestDigest, principalID,
		expectedVersion, now, snapshot,
	)
}

func (service *Service) persistFreshSchedule(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, consumptionID, key, requestDigest, principalID string,
	expectedVersion, now int64,
	snapshot jobSnapshot,
) (Scheduled, error) {
	gameID := snapshot.GameID
	uploadID := snapshot.UploadSessionID
	executionID := snapshot.ExecutionID
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
		return Scheduled{}, ErrInvalid
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
		return Scheduled{}, ErrInvalid
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
		return Scheduled{}, fmt.Errorf("gamecontent/service: %w", err)
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
		return Scheduled{}, fmt.Errorf("gamecontent/service: %w", err)
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
			return Scheduled{}, fmt.Errorf("gamecontent/service: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return Scheduled{}, fmt.Errorf("gamecontent/service: %w", err)
	}
	go service.run(context.WithoutCancel(ctx), jobID, snapshot)
	return result, nil
}

func loadScheduledReplay(
	ctx context.Context,
	transaction *sql.Tx,
	key, principalID, requestDigest string,
	now int64,
) (bool, Scheduled, error) {
	if _, err := transaction.ExecContext(ctx, `
DELETE
FROM idempotency_records
WHERE operation_id='postAdminGameContentRevision'
AND key=?
AND principal_id=?
AND expires_at_ms<=?
`, key, principalID, now); err != nil {
		return false, Scheduled{}, fmt.Errorf("gamecontent/service: %w", err)
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
	if errors.Is(err, sql.ErrNoRows) {
		return false, Scheduled{}, nil
	}
	if err != nil {
		return false, Scheduled{}, fmt.Errorf("gamecontent/service: %w", err)
	}
	if storedDigest != requestDigest {
		return false, Scheduled{}, ErrIdempotencyKeyReused
	}
	var stored Scheduled
	if json.Unmarshal(storedBody, &stored) != nil {
		return false, Scheduled{}, ErrInvalid
	}
	return true, stored, nil
}

func validateReplacementUpload(
	ctx context.Context,
	transaction *sql.Tx,
	uploadID, contentMode, platformID string,
) error {
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
		return ErrInvalid
	}
	var consumed int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM upload_consumptions
WHERE upload_session_id=?
`, uploadID).Scan(&consumed); err != nil ||
		consumed != 0 {
		return ErrInvalid
	}
	return nil
}
