package firmware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/importing"
	"retrom/internal/payloadrelease"
)

var ErrCatalogChanged = errors.New("BIOS_REQUIREMENT_CATALOG_CHANGED")

type ServerInstallRequest struct {
	ServerImportID     string
	JobID              string
	CandidateID        string
	RequirementID      string
	RequirementVersion int64
	ProviderID         string
	TargetID           string
	SourceVersion      string
	CatalogDigest      string
	SourceKind         string
	LogicalName        string
	OriginalFilename   string
	Metadata           blobstore.Metadata
	Status             string
	MatchMethod        string
	Details            map[string]any
	ArchiveEntries     []importing.ArchiveEntry
	ReplaceIfBetter    bool
	StaticExpectation  *StaticExpectation
	StaticEvaluation   *StaticEvaluation
	DATExpectedEntries []ExpectedDATEntry
	DATEvaluation      *DATEvaluation
}

type ServerInstallResult struct {
	Outcome                string
	PreviousInstallationID string
	NewInstallationID      string
	OutcomeCode            string
}

// InstallServerCandidate performs the only active-installation switch for a
// server import. File reads and archive inspection have already completed, so
// the transaction contains only authoritative rechecks and persistence.
//
// Quality comparison, CAS registration and audit commit atomically.
func (service *Service) InstallServerCandidate(
	ctx context.Context,
	request ServerInstallRequest,
) (ServerInstallResult, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ServerInstallResult{}, fmt.Errorf("firmware/server install: %w", err)
	}
	defer cleanup.Rollback(transaction)
	version, err := validateServerCatalog(ctx, transaction, request)
	if err != nil {
		return ServerInstallResult{}, err
	}
	now := service.now().UnixMilli()
	if err := selectServerCandidate(ctx, transaction, request, now); err != nil {
		return ServerInstallResult{}, err
	}
	active, exists, err := readActiveInstallation(ctx, transaction, request.RequirementID)
	if err != nil {
		return ServerInstallResult{}, err
	}
	result, handled, err := service.evaluateExistingInstallation(ctx, transaction, request, version, active, exists)
	if err != nil {
		return ServerInstallResult{}, err
	}
	if handled {
		return service.commitServerOutcome(ctx, transaction, request, result)
	}
	result, err = service.persistServerInstallation(ctx, transaction, request, version, now, result)
	if err != nil {
		return ServerInstallResult{}, err
	}
	return service.commitServerOutcome(ctx, transaction, request, result)
}

func validateServerCatalog(
	ctx context.Context,
	transaction *sql.Tx,
	request ServerInstallRequest,
) (int64, error) {
	var sourceKind, sourceVersion, catalogDigest string
	var requestProviderID, requestTargetID string
	var version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT requirement.source_kind,
requirement.source_version,
requirement.catalog_digest,
requirement.version,
requirement.provider_id,
requirement.target_id
FROM bios_requirements requirement
JOIN runtime_targets target ON target.provider_id=requirement.provider_id
 AND target.target_id=requirement.target_id
WHERE requirement.id=? AND requirement.enabled=1
`, request.RequirementID).Scan(
		&sourceKind, &sourceVersion, &catalogDigest, &version,
		&requestProviderID, &requestTargetID,
	); err != nil ||
		sourceKind != request.SourceKind || sourceVersion != request.SourceVersion ||
		catalogDigest != request.CatalogDigest ||
		version != request.RequirementVersion || requestProviderID != request.ProviderID ||
		requestTargetID != request.TargetID {
		return 0, ErrCatalogChanged
	}
	return version, nil
}

func selectServerCandidate(
	ctx context.Context,
	transaction *sql.Tx,
	request ServerInstallRequest,
	now int64,
) error {
	if changed, updateErr := transaction.ExecContext(ctx, `
UPDATE server_bios_import_candidates
SET state='SELECTED',not_selected_reason=NULL,updated_at_ms=?
WHERE id=? AND server_import_id=? AND requirement_id=? AND state IN ('ELIGIBLE','SELECTED')
`, now, request.CandidateID, request.ServerImportID, request.RequirementID); updateErr != nil {
		return fmt.Errorf("firmware/server select candidate: %w", updateErr)
	} else if rows, rowsErr := changed.RowsAffected(); rowsErr != nil || rows != 1 {
		return fmt.Errorf("firmware/server select candidate changed %d rows: %w", rows, rowsErr)
	}
	return nil
}

type activeInstallation struct {
	id, blobID, filename, md5, sha1, sha256, status string
	size, validatedVersion                          int64
}

func readActiveInstallation(
	ctx context.Context,
	transaction *sql.Tx,
	requirementID string,
) (activeInstallation, bool, error) {
	var active activeInstallation
	err := transaction.QueryRowContext(ctx, `
SELECT id,blob_id,original_filename,size_bytes,md5,sha1,sha256,status,validated_requirement_version
FROM bios_installations WHERE requirement_id=? AND is_active=1
`, requirementID).Scan(
		&active.id, &active.blobID, &active.filename, &active.size, &active.md5,
		&active.sha1, &active.sha256, &active.status, &active.validatedVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return activeInstallation{}, false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return activeInstallation{}, false, fmt.Errorf("firmware/server active: %w", err)
	}
	return active, true, nil
}

func (service *Service) evaluateExistingInstallation(
	ctx context.Context,
	transaction *sql.Tx,
	request ServerInstallRequest,
	version int64,
	active activeInstallation,
	exists bool,
) (ServerInstallResult, bool, error) {
	if !exists {
		return ServerInstallResult{}, false, nil
	}
	result := ServerInstallResult{PreviousInstallationID: active.id}
	if !request.ReplaceIfBetter {
		result.Outcome = "SKIPPED_EXISTING"
		return result, true, nil
	}
	if active.sha256 == request.Metadata.SHA256 {
		if active.validatedVersion == version && active.status == request.Status {
			result.Outcome = "ALREADY_SAME_BYTES"
			return result, true, nil
		}
		return result, false, nil
	}
	activeFacts := FileFacts{
		Basename: active.filename, SizeBytes: active.size, MD5: active.md5, SHA1: active.sha1, SHA256: active.sha256,
	}
	better, complete, err := candidateStrictlyBetter(ctx, transaction, request, active.blobID, activeFacts)
	if err != nil {
		return ServerInstallResult{}, false, err
	}
	if complete && better {
		return result, false, nil
	}
	result.Outcome = "SKIPPED_NOT_BETTER"
	if !complete {
		result.OutcomeCode = "BIOS_CURRENT_EVIDENCE_INCOMPLETE"
	}
	return result, true, nil
}

func (service *Service) persistServerInstallation(
	ctx context.Context,
	transaction *sql.Tx,
	request ServerInstallRequest,
	version, now int64,
	result ServerInstallResult,
) (ServerInstallResult, error) {
	blobID, err := blobstore.EnsureRecord(ctx, transaction, request.Metadata, "application/octet-stream", now)
	if err != nil {
		return ServerInstallResult{}, fmt.Errorf("firmware/server register blob: %w", err)
	}
	if request.SourceKind == "DAT_MACHINE" {
		if err := persistArchiveEntries(ctx, transaction, blobID, request.ArchiveEntries, now); err != nil {
			return ServerInstallResult{}, err
		}
	}
	retired, err := payloadrelease.RetireSupersededBIOS(
		ctx, transaction, request.RequirementID, now,
	)
	if err != nil {
		return ServerInstallResult{}, fmt.Errorf("firmware/server retire: %w", err)
	}
	details := request.Details
	if details == nil {
		details = map[string]any{}
	}
	details["schemaVersion"] = 1
	details["matchMethod"] = request.MatchMethod
	detailsJSON, _ := json.Marshal(details)
	installationID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO bios_installations(
  id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
  validated_requirement_version,status,validation_details_json,is_active,version,
  created_at_ms,updated_at_ms,source_kind,server_import_candidate_id
) VALUES(?,?,?,?,?,?,?,?,?,?,?,1,1,?,?,'SERVER_DIRECTORY',?)
`, installationID.String(), request.RequirementID, blobID, request.OriginalFilename,
		request.Metadata.Size, request.Metadata.MD5, request.Metadata.SHA1, request.Metadata.SHA256,
		version, request.Status, string(detailsJSON), now, now, request.CandidateID); err != nil {
		return ServerInstallResult{}, fmt.Errorf("firmware/server persist installation: %w", err)
	}
	if service.releases != nil {
		if err := service.releases.StageCandidates(ctx, transaction, retired.BlobIDs); err != nil {
			return ServerInstallResult{}, fmt.Errorf("firmware/server stage retired: %w", err)
		}
	}
	result.NewInstallationID = installationID.String()
	switch request.Status {
	case "MATCHED":
		result.Outcome = "IMPORTED_MATCHED"
	case "HASH_WARNING":
		result.Outcome = "IMPORTED_WARNING"
	default:
		result.Outcome = "IMPORTED_MISSING_ENTRY"
	}
	return result, nil
}

func (service *Service) commitServerOutcome(
	ctx context.Context,
	transaction *sql.Tx,
	request ServerInstallRequest,
	result ServerInstallResult,
) (ServerInstallResult, error) {
	code := result.OutcomeCode
	if code == "" {
		switch result.Outcome {
		case "SKIPPED_EXISTING":
			code = "BIOS_EXISTING_INSTALLATION_PRESERVED"
		case "SKIPPED_NOT_BETTER":
			code = "BIOS_CANDIDATE_NOT_BETTER"
		case "ALREADY_SAME_BYTES":
			code = "BIOS_INSTALLATION_SAME_BYTES"
		default:
			code = result.Outcome
		}
	}
	now := service.now().UnixMilli()
	details := request.Details
	if details == nil {
		details = map[string]any{}
	}
	detailsJSON, _ := json.Marshal(details)
	nullable := func(value string) any {
		if value == "" {
			return nil
		}
		return value
	}
	if changed, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state=?,match_method=?,selection_details_json=?,outcome_code=?,
previous_installation_id=?,new_installation_id=?,completed_at_ms=?,updated_at_ms=?
WHERE server_import_id=? AND requirement_id=? AND state IN ('PENDING','EVALUATING')
`, result.Outcome, request.MatchMethod, string(detailsJSON), code, nullable(result.PreviousInstallationID),
		nullable(result.NewInstallationID), now, now, request.ServerImportID, request.RequirementID); err != nil {
		return ServerInstallResult{}, fmt.Errorf("firmware/server item result: %w", err)
	} else if rows, rowsErr := changed.RowsAffected(); rowsErr != nil || rows != 1 {
		return ServerInstallResult{}, fmt.Errorf("firmware/server item result changed %d rows: %w", rows, rowsErr)
	}
	eventJSON, _ := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"phase":         "INSTALLING",
		"result":        result.Outcome,
	})
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'SERVER_IMPORT',?,'PROGRESS',?,?)
`, request.JobID, request.ServerImportID, string(eventJSON), now); err != nil {
		return ServerInstallResult{}, fmt.Errorf("firmware/server item event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return ServerInstallResult{}, fmt.Errorf("firmware/server commit: %w", err)
	}
	if service.releases != nil {
		service.releases.Signal()
	}
	return result, nil
}

func candidateStrictlyBetter(
	ctx context.Context,
	transaction *sql.Tx,
	request ServerInstallRequest,
	activeBlobID string,
	activeFacts FileFacts,
) (bool, bool, error) {
	if request.SourceKind == "STATIC" {
		if request.StaticExpectation == nil || request.StaticEvaluation == nil {
			return false, false, nil
		}
		active := EvaluateStatic(*request.StaticExpectation, activeFacts)
		return CompareStaticQuality(*request.StaticEvaluation, active) < 0, true, nil
	}
	if request.DATEvaluation == nil || len(request.DATExpectedEntries) == 0 {
		return false, false, nil
	}
	entries, err := archiveEntriesForBlob(ctx, transaction, activeBlobID)
	if err != nil {
		return false, false, err
	}
	if len(entries) == 0 {
		return false, false, nil
	}
	active := EvaluateDAT(request.LogicalName, request.DATExpectedEntries, activeFacts, entries)
	return CompareDATQuality(*request.DATEvaluation, active) < 0, true, nil
}

func archiveEntriesForBlob(ctx context.Context, transaction *sql.Tx, blobID string) ([]importing.ArchiveEntry, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT ordinal,original_relative_path,normalized_path,ascii_casefold_path,archive_format,
compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256
FROM archive_entries WHERE archive_blob_id=? ORDER BY ordinal`, blobID)
	if err != nil {
		return nil, fmt.Errorf("firmware/server active archive: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]importing.ArchiveEntry, 0)
	for rows.Next() {
		var entry importing.ArchiveEntry
		if err := rows.Scan(&entry.Ordinal, &entry.OriginalPath, &entry.NormalizedPath, &entry.ASCIICasefoldPath,
			&entry.ArchiveFormat, &entry.CompressionProfile, &entry.Size, &entry.CRC32, &entry.MD5, &entry.SHA1,
			&entry.SHA256); err != nil {
			return nil, fmt.Errorf("firmware/server active archive: %w", err)
		}
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("firmware/server iterate active archive: %w", err)
	}
	return result, nil
}
