package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/tagging"
)

type importGroupTargetGuard struct {
	ProviderID            string `json:"providerId"`
	TargetID              string `json:"targetId"`
	TargetContractSHA256  string `json:"targetContractSha256"`
	GameCompatibilityLine string `json:"gameCompatibilityLine"`
	CoreID                string `json:"coreId"`
}

type importGroupTargetSnapshot struct {
	SchemaVersion           int                      `json:"schemaVersion"`
	DefaultCoreID           string                   `json:"defaultCoreId"`
	PlatformID              string                   `json:"platformId"`
	PlatformInstanceID      string                   `json:"platformInstanceId"`
	PlatformInstanceVersion int64                    `json:"platformInstanceVersion"`
	Targets                 []importGroupTargetGuard `json:"targets"`
}

type queuedImportGroupRequest struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Request       CreateRequest       `json:"request"`
	Tags          []tagging.Reference `json:"tags"`
}

type importGroupAdmission struct {
	request        CreateRequest
	contentMode    string
	provisional    creationTarget
	targetSnapshot importGroupTargetSnapshot
	files          []importSourceFile
	uploadVersion  int64
	manifestDigest string
}

// QueueCreate persists the immutable import input and returns before archive
// scanning, content identification, and CAS materialization begin.
func (service *Service) QueueCreate(ctx context.Context, rawRequest CreateRequest) (Created, error) {
	admission, err := service.admitImportGroup(ctx, rawRequest)
	if err != nil {
		return Created{}, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: %w", err)
	}
	defer cleanup.Rollback(transaction)
	actor := reviewActor(ctx)
	actorUserID, actorIsUser := actor.UserID.(string)
	if len(admission.request.TagIDs) > 0 && (!actorIsUser || actorUserID == "") {
		return Created{}, ErrInvalid
	}
	tags, err := service.tags.ValidateReferences(ctx, transaction, admission.request.TagIDs)
	if err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: validate import tags: %w", err)
	}
	created, err := service.insertQueuedImportGroup(
		ctx, transaction, admission.request, admission.contentMode, admission.provisional,
		admission.targetSnapshot, tags, actorUserID, admission.uploadVersion,
		admission.manifestDigest, admission.files,
	)
	if err != nil {
		return Created{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: %w", err)
	}
	service.scheduleImportGroup(ctx, created.JobID, 0)
	return created, nil
}

func (service *Service) admitImportGroup(
	ctx context.Context,
	rawRequest CreateRequest,
) (importGroupAdmission, error) {
	request, contentMode, err := normalizeCreateRequest(rawRequest)
	if err != nil {
		return importGroupAdmission{}, err
	}
	purpose, sourceType, uploadVersion, manifestDigest, err := service.loadQueuedUpload(ctx, request.UploadID)
	if err != nil {
		return importGroupAdmission{}, err
	}
	target, err := service.loadCreationTarget(ctx, request.TargetPlatformInstanceID)
	if err != nil {
		return importGroupAdmission{}, err
	}
	files, err := service.loadImportSourceFiles(ctx, request.UploadID)
	if err != nil || len(files) == 0 {
		return importGroupAdmission{}, ErrInvalid
	}
	request, contentMode, err = normalizeTargetCreateRequest(
		request, contentMode, purpose, sourceType, files, target,
	)
	if err != nil {
		return importGroupAdmission{}, err
	}
	if request.MetadataProvider == "HASHEOUS" && service.scraper == nil {
		return importGroupAdmission{}, fmt.Errorf("libraryimport/service: %w", errMetadataScraperNotConfigured)
	}
	if err := validateCreationUpload(contentMode, sourceType, purpose); err != nil {
		return importGroupAdmission{}, err
	}
	capabilities := contentcapability.Resolve(
		target.platformID, true, service.multiDiscImportEnabled, target.contentPolicyJSON,
	)
	if contentMode == contentcapability.ModeMultiDisc && capabilities.MultiDisc == nil {
		return importGroupAdmission{}, ErrMultiDiscModeUnavailable
	}
	targetSnapshot, provisional, err := service.importGroupTargetSnapshot(ctx, request, target)
	if err != nil {
		return importGroupAdmission{}, err
	}
	return importGroupAdmission{
		request: request, contentMode: contentMode, provisional: provisional,
		targetSnapshot: targetSnapshot, files: files, uploadVersion: uploadVersion,
		manifestDigest: manifestDigest,
	}, nil
}

func (service *Service) loadQueuedUpload(
	ctx context.Context,
	uploadID string,
) (string, string, int64, string, error) {
	var purpose, sourceType, state, manifestDigest string
	var version int64
	err := service.database.QueryRowContext(ctx, `
SELECT purpose,source_type,state,version,manifest_digest
FROM upload_sessions WHERE id=?
`, uploadID).Scan(&purpose, &sourceType, &state, &version, &manifestDigest)
	if err != nil || state != "COMPLETE" {
		return "", "", 0, "", ErrInvalid
	}
	return purpose, sourceType, version, manifestDigest, nil
}

func (service *Service) importGroupTargetSnapshot(
	ctx context.Context,
	request CreateRequest,
	target creationTarget,
) (importGroupTargetSnapshot, creationTarget, error) {
	snapshot := importGroupTargetSnapshot{
		SchemaVersion: 1, DefaultCoreID: target.defaultCoreID, PlatformID: target.platformID,
		PlatformInstanceID:      request.TargetPlatformInstanceID,
		PlatformInstanceVersion: target.instanceVersion,
	}
	if target.providerID != "" {
		snapshot.Targets = []importGroupTargetGuard{targetGuard(target)}
		return snapshot, target, nil
	}
	if target.platformID != "rpgmaker" || target.defaultCoreID != detector.VirtualCoreID {
		return importGroupTargetSnapshot{}, creationTarget{}, ErrInvalid
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT binding.core_id,binding.provider_id,binding.target_id,target.target_contract_sha256,
 target.game_compatibility_line
FROM runtime_target_bindings binding
JOIN runtime_binding_platforms platform ON platform.binding_id=binding.binding_id AND platform.platform_id='rpgmaker'
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE binding.core_id='rpgmaker' AND binding.launch_policy!='DISABLED'
ORDER BY binding.detector_profile,binding.provider_id,binding.target_id
`)
	if err != nil {
		return importGroupTargetSnapshot{}, creationTarget{}, fmt.Errorf("libraryimport/queue: list RPG targets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var guard importGroupTargetGuard
		if err := rows.Scan(
			&guard.CoreID, &guard.ProviderID, &guard.TargetID,
			&guard.TargetContractSHA256, &guard.GameCompatibilityLine,
		); err != nil {
			return importGroupTargetSnapshot{}, creationTarget{}, fmt.Errorf("libraryimport/queue: scan RPG target: %w", err)
		}
		snapshot.Targets = append(snapshot.Targets, guard)
	}
	if err := rows.Err(); err != nil {
		return importGroupTargetSnapshot{}, creationTarget{}, fmt.Errorf("libraryimport/queue: list RPG targets: %w", err)
	}
	if len(snapshot.Targets) != 7 {
		return importGroupTargetSnapshot{}, creationTarget{}, ErrInvalid
	}
	sort.Slice(snapshot.Targets, func(left, right int) bool {
		return snapshot.Targets[left].ProviderID+"\x00"+snapshot.Targets[left].TargetID <
			snapshot.Targets[right].ProviderID+"\x00"+snapshot.Targets[right].TargetID
	})
	provisional := target
	provisional.providerID = snapshot.Targets[0].ProviderID
	provisional.targetID = snapshot.Targets[0].TargetID
	provisional.targetContractSHA256 = snapshot.Targets[0].TargetContractSHA256
	provisional.gameCompatibilityLine = snapshot.Targets[0].GameCompatibilityLine
	provisional.coreID = snapshot.Targets[0].CoreID
	return snapshot, provisional, nil
}

func targetGuard(target creationTarget) importGroupTargetGuard {
	return importGroupTargetGuard{
		ProviderID: target.providerID, TargetID: target.targetID,
		TargetContractSHA256:  target.targetContractSHA256,
		GameCompatibilityLine: target.gameCompatibilityLine, CoreID: target.coreID,
	}
}

func (service *Service) insertQueuedImportGroup(
	ctx context.Context,
	transaction *sql.Tx,
	request CreateRequest,
	contentMode string,
	provisional creationTarget,
	targetSnapshot importGroupTargetSnapshot,
	tags []tagging.Reference,
	actorUserID string,
	uploadVersion int64,
	manifestDigest string,
	files []importSourceFile,
) (Created, error) {
	importUUID, _ := uuid.NewV7()
	jobUUID, _ := uuid.NewV7()
	executionUUID, _ := uuid.NewV7()
	importID, jobID := importUUID.String(), jobUUID.String()
	now := service.now().UnixMilli()
	requestDocument := queuedImportGroupRequest{SchemaVersion: 1, Request: request, Tags: tags}
	requestJSON, requestDigest := marshaledDigest(requestDocument)
	targetJSON, targetDigest := marshaledDigest(targetSnapshot)
	configJSON, configDigest := marshaledDigest(map[string]any{
		"schemaVersion": 3, "bindingState": "PENDING", "contentMode": contentMode,
		"platformInstanceId":      request.TargetPlatformInstanceID,
		"platformInstanceVersion": provisional.instanceVersion, "platformId": provisional.platformID,
		"defaultCoreId": provisional.defaultCoreID, "resolvedCoreId": nil,
		"providerId": provisional.providerID, "targetId": provisional.targetID,
		"targetContractSha256":          provisional.targetContractSHA256,
		"metadataProviderConfigVersion": 1, "tags": tags,
	})
	dedupe := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00IMPORT_GROUP\x00" + importID))
	inputJSON, inputDigest := marshaledDigest(map[string]any{
		"schemaVersion": 1, "kind": "IMPORT_GROUP",
		"scope":       map[string]any{"type": "IMPORT_GROUP", "id": importID},
		"executionId": executionUUID.String(),
		"inputs": map[string]any{
			"uploadSessionId": request.UploadID, "uploadVersion": uploadVersion,
			"manifestDigest": manifestDigest, "importConfigSnapshotDigest": requestDigest,
		},
	})
	_, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(
 id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms
) VALUES(?,'IMPORT_GROUP',?,'IMPORT_GROUP',?,1,'{"schemaVersion":1,"inputExecutionNo":1}',1,
 'QUEUED',0,4,?,?,?)
`, jobID, importID, hex.EncodeToString(dedupe[:]), now, now, now)
	if err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: insert job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)
`, jobID, string(inputJSON), inputDigest, now); err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: insert job input: %w", err)
	}
	_, err = transaction.ExecContext(ctx, insertImportJobSQL,
		importID, request.UploadID, request.TargetPlatformInstanceID,
		provisional.instanceVersion, provisional.platformID, provisional.defaultCoreID,
		provisional.providerID, provisional.targetID, provisional.targetContractSHA256,
		nil, request.MetadataProvider, string(configJSON), configDigest,
		"QUEUED", 0, 0, 0, 0, 0, now, now, nil,
	)
	if err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: insert import: %w", err)
	}
	var actor any
	if actorUserID != "" {
		actor = actorUserID
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_group_requests(
 import_job_id,schema_version,request_json,request_digest,actor_user_id,upload_version,
 upload_manifest_digest,target_snapshot_json,target_snapshot_digest,created_at_ms
) VALUES(?,1,?,?,?,?,?,?,?,?)
`, importID, string(requestJSON), requestDigest, actor, uploadVersion, manifestDigest,
		string(targetJSON), targetDigest, now); err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: insert request: %w", err)
	}
	consumptionID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,NULL,'IMPORT_JOB',?,?)
`, consumptionID.String(), request.UploadID, importID, now); err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: insert upload consumption: %w", err)
	}
	for _, file := range files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO import_job_files(
 import_job_id,upload_file_id,disposition,reason_code,created_at_ms,updated_at_ms
) VALUES(?,?,'PENDING',NULL,?,?)
`, importID, file.id, now, now); err != nil {
			return Created{}, fmt.Errorf("libraryimport/queue: insert pending file: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_GROUP',?,'QUEUED',
 json_object('schemaVersion',1,'executionNo',1,'attempt',0,'state','QUEUED',
 'phase','WAITING_FOR_WORKER'),?)
`, jobID, importID, now); err != nil {
		return Created{}, fmt.Errorf("libraryimport/queue: insert event: %w", err)
	}
	return Created{ImportJobID: importID, JobID: jobID, State: "QUEUED", ItemCount: 0}, nil
}

func marshaledDigest(value any) ([]byte, string) {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(digest[:])
}

func queuedPrincipalContext(ctx context.Context, userID string) context.Context {
	if userID == "" {
		return ctx
	}
	return authn.WithPrincipal(ctx, authn.Principal{UserID: userID})
}
