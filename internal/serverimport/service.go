package serverimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/firmware"
	retromruntime "retrom/internal/runtime"
)

var (
	ErrActive         = errors.New("SERVER_BIOS_IMPORT_ACTIVE")
	ErrCatalogEmpty   = errors.New("BIOS_CATALOG_EMPTY")
	ErrCatalogInvalid = errors.New("BIOS_CATALOG_INVALID")
	ErrScanLimit      = errors.New("SERVER_IMPORT_SCAN_LIMIT_EXCEEDED")
	ErrNotCancellable = errors.New("SERVER_IMPORT_NOT_CANCELLABLE")
	ErrNotRetryable   = errors.New("SERVER_IMPORT_NOT_RETRYABLE")
	ErrNotFound       = errors.New("SERVER_IMPORT_NOT_FOUND")
)

type Root struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	path   string
	digest string
}

type CreateRequest struct {
	Kind               string `json:"kind"`
	RootID             string `json:"rootId"`
	SourceRelativePath string `json:"sourceRelativePath"`
	ReplaceIfBetter    bool   `json:"replaceIfBetter"`
}

type Counts struct {
	CatalogItems   int64 `json:"catalogItems"`
	Candidates     int64 `json:"candidates"`
	EvaluatedItems int64 `json:"evaluatedItems"`
	Imported       int64 `json:"imported"`
	Matched        int64 `json:"matched"`
	Warnings       int64 `json:"warnings"`
	NotFound       int64 `json:"notFound"`
	Skipped        int64 `json:"skipped"`
	Conflicts      int64 `json:"conflicts"`
	Failed         int64 `json:"failed"`
	Cancelled      int64 `json:"cancelled"`
}

type Summary struct {
	ID                 string    `json:"id"`
	Kind               string    `json:"kind"`
	Root               RootRef   `json:"root"`
	SourceRelativePath string    `json:"sourceRelativePath"`
	ReplaceIfBetter    bool      `json:"replaceIfBetter"`
	State              string    `json:"state"`
	Phase              *string   `json:"phase"`
	Counts             Counts    `json:"counts"`
	JobID              string    `json:"jobId"`
	CreatedBy          CreatedBy `json:"createdBy"`
	LastErrorCode      *string   `json:"lastErrorCode"`
	Version            int64     `json:"version"`
	CreatedAtMS        int64     `json:"createdAtMs"`
	UpdatedAtMS        int64     `json:"updatedAtMs"`
	CompletedAtMS      *int64    `json:"completedAtMs"`
}

type RootRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type CreatedBy struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Item struct {
	RequirementID              string         `json:"requirementId"`
	CoreID                     string         `json:"coreId"`
	CoreName                   string         `json:"coreName"`
	CoreArtifactID             string         `json:"coreArtifactId"`
	LogicalName                string         `json:"logicalName"`
	RequirementMode            string         `json:"requirementMode"`
	SourceKind                 string         `json:"sourceKind"`
	State                      string         `json:"state"`
	CandidateCount             int64          `json:"candidateCount"`
	MatchMethod                *string        `json:"matchMethod"`
	OutcomeCode                *string        `json:"outcomeCode"`
	SelectedRelativePath       *string        `json:"selectedRelativePath"`
	PreviousInstallationStatus *string        `json:"previousInstallationStatus"`
	NewInstallationStatus      *string        `json:"newInstallationStatus"`
	Replaced                   bool           `json:"replaced"`
	SelectionDetails           map[string]any `json:"selectionDetails,omitempty"`
}

type Candidate struct {
	ID                string         `json:"id"`
	RelativePath      string         `json:"relativePath"`
	Basename          string         `json:"basename"`
	AssociationKind   string         `json:"associationKind"`
	SizeBytes         int64          `json:"sizeBytes"`
	MD5               *string        `json:"md5"`
	SHA1              *string        `json:"sha1"`
	SHA256            *string        `json:"sha256"`
	CRC32             *string        `json:"crc32"`
	State             string         `json:"state"`
	RankOrdinal       *int64         `json:"rankOrdinal"`
	NotSelectedReason *string        `json:"notSelectedReason"`
	EvaluationDetails map[string]any `json:"evaluationDetails,omitempty"`
}

type Service struct {
	database    *sql.DB
	blobs       *blobstore.Store
	firmware    *firmware.Service
	credentials *retromruntime.Credentials
	roots       map[string]Root
	now         func() time.Time
	wake        chan struct{}
	stop        chan struct{}
	stopOnce    sync.Once
	archiveScan chan struct{}
	scanLimits  scanLimits
}

type scanLimits struct {
	maxDepth                    int
	maxDirectories              int64
	maxFiles                    int64
	maxPhysicalCandidates       int64
	maxCandidatesPerRequirement int
	maxHashedBytes              int64
	hashWorkers                 int
}

func defaultScanLimits() scanLimits {
	return scanLimits{
		maxDepth:                    64,
		maxDirectories:              250000,
		maxFiles:                    2000000,
		maxPhysicalCandidates:       100000,
		maxCandidatesPerRequirement: 10000,
		maxHashedBytes:              2 << 40,
		hashWorkers:                 2,
	}
}

func New(
	database *sql.DB,
	blobs *blobstore.Store,
	firmwareService *firmware.Service,
	credentials *retromruntime.Credentials,
	configured []config.ServerImportRoot,
	now func() time.Time,
) *Service {
	roots := make(map[string]Root, len(configured))
	for _, configuredRoot := range configured {
		digest := credentials.ServerImportRootDigest(configuredRoot.ID, configuredRoot.CanonicalPath)
		roots[configuredRoot.ID] = Root{
			ID: configuredRoot.ID, Label: configuredRoot.Label, path: configuredRoot.CanonicalPath,
			digest: hex.EncodeToString(digest[:]),
		}
	}
	return &Service{
		database: database, blobs: blobs, firmware: firmwareService, credentials: credentials,
		roots: roots, now: now, wake: make(chan struct{}, 1), stop: make(chan struct{}), archiveScan: make(chan struct{}, 1),
		scanLimits: defaultScanLimits(),
	}
}

func (service *Service) Start() {
	go service.runLoop()
	service.signal()
}

func (service *Service) Close() { service.stopOnce.Do(func() { close(service.stop) }) }

func (service *Service) Roots() []Root {
	result := make([]Root, 0, len(service.roots))
	for _, root := range service.roots {
		root.Status = "AVAILABLE"
		opened, err := openDirectoryNoFollow(root.path)
		if err != nil {
			root.Status = "UNAVAILABLE"
		} else {
			cleanup.Error("close", opened.Close())
		}
		root.path, root.digest = "", ""
		result = append(result, root)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}

func (service *Service) Directories(rootID, relativePath string) ([]Directory, error) {
	if err := ValidateRootID(rootID); err != nil {
		return nil, err
	}
	root, ok := service.roots[rootID]
	if !ok {
		return nil, ErrRootNotFound
	}
	if err := ValidateRelativePath(relativePath); err != nil {
		return nil, err
	}
	return listDirectories(root.path, relativePath)
}

type catalogItem struct {
	State                     string  `json:"-"`
	RequirementID             string  `json:"requirementId"`
	RequirementVersion        int64   `json:"requirementVersion"`
	CoreID                    string  `json:"coreId"`
	CoreName                  string  `json:"coreName"`
	CoreArtifactID            string  `json:"coreArtifactId"`
	CoreArtifactVersion       int64   `json:"coreArtifactVersion"`
	SourceKind                string  `json:"sourceKind"`
	LogicalName               string  `json:"logicalName"`
	RequirementMode           string  `json:"requirementMode"`
	ConditionCode             *string `json:"conditionCode"`
	ActivationOptionsJSON     *string `json:"activationOptionsJson"`
	DeliveryKind              string  `json:"deliveryKind"`
	EmulatorPath              *string `json:"emulatorPath"`
	SourceVersion             string  `json:"sourceVersion"`
	CatalogDigest             string  `json:"catalogDigest"`
	DATVersionID              *string `json:"datVersionId"`
	DATMachineName            *string `json:"datMachineName"`
	ExpectedSize              *int64  `json:"expectedSizeBytes"`
	ExpectedMD5               *string `json:"expectedMd5"`
	ExpectedSHA1              *string `json:"expectedSha1"`
	ExpectedSHA256            *string `json:"expectedSha256"`
	ActiveInstallationID      *string `json:"activeInstallationId"`
	ActiveInstallationVersion *int64  `json:"activeInstallationVersion"`
	ActiveBlobSHA256          *string `json:"activeBlobSha256"`
	ActiveStatus              *string `json:"activeStatus"`
	ActiveValidatedVersion    *int64  `json:"activeValidatedRequirementVersion"`
}

//nolint:funlen,gocyclo // Validation, catalog freezing and task/item creation share one atomic create contract.
func (service *Service) Create(ctx context.Context, request CreateRequest, userID string) (Summary, error) {
	if request.Kind != "BIOS_DIRECTORY" {
		return Summary{}, ErrCatalogInvalid
	}
	if err := ValidateRootID(request.RootID); err != nil {
		return Summary{}, err
	}
	root, ok := service.roots[request.RootID]
	if !ok {
		return Summary{}, ErrRootNotFound
	}
	if err := ValidateRelativePath(request.SourceRelativePath); err != nil {
		return Summary{}, err
	}
	directory, err := openSelectedDirectory(root.path, request.SourceRelativePath)
	if err != nil {
		return Summary{}, ErrRootUnavailable
	}
	cleanup.Error("close", directory.Close())
	items, err := service.freezeCatalog(ctx)
	if err != nil {
		return Summary{}, err
	}
	if len(items) == 0 {
		return Summary{}, ErrCatalogEmpty
	}
	encoded, err := canonicalCatalogJSON(items)
	if err != nil {
		return Summary{}, fmt.Errorf("serverimport/catalog snapshot: %w", err)
	}
	digest := sha256.Sum256(encoded)
	catalogDigest := hex.EncodeToString(digest[:])
	importID, _ := uuid.NewV7()
	jobID, _ := uuid.NewV7()
	executionID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	input := map[string]any{
		"schemaVersion": 1, "kind": "SERVER_BIOS_IMPORT",
		"scope":       map[string]any{"type": "SERVER_IMPORT", "id": importID.String()},
		"executionId": executionID.String(),
		"inputs": map[string]any{
			"serverImportVersion": 1, "rootId": root.ID, "sourceRelativePath": request.SourceRelativePath,
			"rootConfigDigest": root.digest, "catalogSnapshotDigest": catalogDigest,
			"replaceIfBetter": request.ReplaceIfBetter,
		},
	}
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	dedupe := sha256.Sum256([]byte(importID.String()))
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("serverimport/begin create transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'SERVER_IMPORT',?,'SERVER_BIOS_IMPORT',?,1,'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?)
`, jobID.String(), importID.String(), hex.EncodeToString(dedupe[:]), now, now, now); err != nil {
		if strings.Contains(err.Error(), "server_imports_one_active_kind") {
			return Summary{}, ErrActive
		}
		return Summary{}, fmt.Errorf("serverimport/create job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES(?,1,?,?,?)
`, jobID.String(), string(inputJSON), hex.EncodeToString(inputDigest[:]), now); err != nil {
		return Summary{}, fmt.Errorf("serverimport/create input snapshot: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO server_imports(id,kind,root_id,root_label_snapshot,source_relative_path,root_config_digest,
catalog_snapshot_digest,replace_if_better,state,catalog_item_count,job_id,created_by_user_id,
version,created_at_ms,updated_at_ms)
VALUES(?,'BIOS_DIRECTORY',?,?,?,?,?,?,'QUEUED',?,?,?,?,?,?)
`, importID.String(), root.ID, root.Label, request.SourceRelativePath, root.digest, catalogDigest,
		boolInteger(request.ReplaceIfBetter), len(items), jobID.String(), userID, 1, now, now); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: server_imports.kind") {
			return Summary{}, ErrActive
		}
		return Summary{}, fmt.Errorf("serverimport/create: %w", err)
	}
	for _, item := range items {
		if err := insertCatalogItem(ctx, transaction, importID.String(), item, now); err != nil {
			return Summary{}, fmt.Errorf("serverimport/create catalog item: %w", err)
		}
	}
	eventID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'QUEUED','{"schemaVersion":1}',?)
`, jobID.String(), importID.String(), now); err != nil {
		return Summary{}, fmt.Errorf("serverimport/create queued event: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'SERVER_IMPORT_CREATED','SERVER_IMPORT',?,NULL,?,NULL,NULL,?)
`, eventID.String(), userID, importID.String(), `{"state":"QUEUED"}`, now); err != nil {
		return Summary{}, fmt.Errorf("serverimport/create audit event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, fmt.Errorf("serverimport/commit create transaction: %w", err)
	}
	service.signal()
	return service.Get(ctx, importID.String())
}

func canonicalCatalogJSON(items []catalogItem) ([]byte, error) {
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("marshal catalog snapshot: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode catalog snapshot for canonicalization: %w", err)
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode canonical catalog snapshot: %w", err)
	}
	return bytes.TrimSuffix(output.Bytes(), []byte("\n")), nil
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

//nolint:lll // Catalog validation keeps source readiness predicates together for auditability.
func (service *Service) freezeCatalog(ctx context.Context) ([]catalogItem, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT requirement.id,requirement.version,requirement.core_id,core.name,requirement.core_artifact_id,
artifact.version,requirement.source_kind,requirement.logical_name,requirement.requirement_mode,
requirement.condition_code,requirement.activation_options_json,requirement.delivery_kind,requirement.emulator_path,
requirement.source_version,requirement.catalog_digest,
CASE WHEN requirement.source_kind='DAT_MACHINE' THEN dat.id END,requirement.dat_machine_name,
requirement.size_bytes,requirement.md5,requirement.sha1,requirement.sha256,
installation.id,installation.version,blob.sha256,installation.status,installation.validated_requirement_version,
dat.parse_status,dat.is_active
FROM bios_requirements requirement
JOIN cores core ON core.id=requirement.core_id
JOIN core_artifacts artifact ON artifact.id=requirement.core_artifact_id AND artifact.enabled=1
LEFT JOIN dat_versions dat ON dat.id=requirement.source_version AND dat.core_artifact_id=requirement.core_artifact_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
 AND installation.is_active=1
LEFT JOIN blobs blob ON blob.id=installation.blob_id
WHERE requirement.enabled=1
ORDER BY requirement.id COLLATE BINARY
`)
	if err != nil {
		return nil, fmt.Errorf("serverimport/query catalog: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]catalogItem, 0)
	for rows.Next() {
		var item catalogItem
		var datStatus sql.NullString
		var datActive sql.NullInt64
		if err := rows.Scan(
			&item.RequirementID, &item.RequirementVersion, &item.CoreID, &item.CoreName, &item.CoreArtifactID,
			&item.CoreArtifactVersion, &item.SourceKind, &item.LogicalName, &item.RequirementMode,
			&item.ConditionCode, &item.ActivationOptionsJSON, &item.DeliveryKind, &item.EmulatorPath,
			&item.SourceVersion, &item.CatalogDigest, &item.DATVersionID,
			&item.DATMachineName, &item.ExpectedSize, &item.ExpectedMD5, &item.ExpectedSHA1, &item.ExpectedSHA256,
			&item.ActiveInstallationID, &item.ActiveInstallationVersion, &item.ActiveBlobSHA256, &item.ActiveStatus,
			&item.ActiveValidatedVersion, &datStatus, &datActive,
		); err != nil {
			return nil, fmt.Errorf("serverimport/scan catalog item: %w", err)
		}
		if item.SourceKind == "STATIC" && item.ExpectedMD5 == nil && item.ExpectedSHA1 == nil && item.ExpectedSHA256 == nil {
			return nil, ErrCatalogInvalid
		}
		if item.SourceKind == "DAT_MACHINE" && (item.DATVersionID == nil || !datStatus.Valid || datStatus.String != "READY" || !datActive.Valid || datActive.Int64 != 1) {
			return nil, ErrCatalogInvalid
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("serverimport/iterate catalog: %w", err)
	}
	return items, nil
}

func insertCatalogItem(ctx context.Context, transaction *sql.Tx, importID string, item catalogItem, now int64) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO server_bios_import_items(
server_import_id,requirement_id,requirement_version,core_id,core_name_snapshot,core_artifact_id,
core_artifact_version,source_kind,logical_name,requirement_mode,condition_code,delivery_kind,emulator_path,
activation_options_json,source_version,catalog_digest,dat_version_id,dat_machine_name,expected_size_bytes,
expected_md5,expected_sha1,expected_sha256,
active_installation_id_snapshot,active_installation_version_snapshot,active_blob_sha256_snapshot,
active_status_snapshot,active_validated_requirement_version_snapshot,state,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'PENDING',?,?)
`, importID, item.RequirementID, item.RequirementVersion, item.CoreID, item.CoreName, item.CoreArtifactID,
		item.CoreArtifactVersion, item.SourceKind, item.LogicalName, item.RequirementMode, item.ConditionCode,
		item.DeliveryKind, item.EmulatorPath, item.ActivationOptionsJSON, item.SourceVersion, item.CatalogDigest,
		item.DATVersionID, item.DATMachineName, item.ExpectedSize,
		item.ExpectedMD5, item.ExpectedSHA1, item.ExpectedSHA256, item.ActiveInstallationID, item.ActiveInstallationVersion,
		item.ActiveBlobSHA256, item.ActiveStatus, item.ActiveValidatedVersion, now, now)
	if err != nil {
		return fmt.Errorf("serverimport/catalog item: %w", err)
	}
	return nil
}

func (service *Service) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}
