package pegasusimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/serversource"
)

const summaryQuery = `
SELECT import.id,import.root_id,import.root_label_snapshot,import.source_relative_path,import.state,import.phase,
import.scan_job_id,import.import_job_id,import.metadata_count,import.invalid_metadata_count,import.collection_count,
import.game_count,import.estimated_source_bytes,import.mapped_collection_count,import.skipped_collection_count,
import.processable_item_count,import.blocked_item_count,import.published_item_count,import.existing_item_count,
import.failed_item_count,import.cancelled_item_count,import.media_warning_count,import.discovered_cover_count,
import.discovered_video_count,import.mapping_version,import.version,import.created_by_user_id,user.display_name,
import.last_error_code,import.retryable,import.created_at_ms,import.updated_at_ms,import.expires_at_ms,import.completed_at_ms
FROM pegasus_imports import JOIN users user ON user.id=import.created_by_user_id`

type rowScanner interface{ Scan(...any) error }

func scanSummary(row rowScanner) (Summary, error) {
	var result Summary
	var importJobID, phase, errorCode sql.NullString
	var retryable int
	if err := row.Scan(
		&result.ID, &result.Root.ID, &result.Root.Label, &result.SourceRelativePath, &result.State, &phase,
		&result.ScanJobID, &importJobID, &result.Counts.Metadata, &result.Counts.InvalidMetadata,
		&result.Counts.Collections, &result.Counts.Games, &result.Counts.EstimatedSourceBytes,
		&result.Counts.MappedCollections, &result.Counts.SkippedCollections, &result.Counts.Processable,
		&result.Counts.Blocked, &result.Counts.Published, &result.Counts.Existing, &result.Counts.Failed,
		&result.Counts.Cancelled, &result.Counts.MediaWarnings, &result.Counts.Covers, &result.Counts.Videos,
		&result.MappingVersion, &result.Version, &result.CreatedBy.ID, &result.CreatedBy.DisplayName,
		&errorCode, &retryable, &result.CreatedAtMS, &result.UpdatedAtMS, &result.ExpiresAtMS, &result.CompletedAtMS,
	); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/scan summary: %w", err)
	}
	result.Phase = nullableString(phase)
	result.ImportJobID = nullableString(importJobID)
	result.LastErrorCode = nullableString(errorCode)
	result.Retryable = retryable == 1
	return result, nil
}

func (service *Service) Get(ctx context.Context, importID string) (Summary, error) {
	result, err := scanSummary(service.database.QueryRowContext(ctx, summaryQuery+` WHERE import.id=?`, importID))
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	return result, err
}

func (service *Service) List(
	ctx context.Context,
	state string,
	beforeAt int64,
	beforeID string,
	limit int,
) ([]Summary, error) {
	if limit < 1 || limit > 21 {
		return nil, ErrInvalid
	}
	conditions := []string{"1=1"}
	arguments := []any{}
	if state != "" {
		conditions = append(conditions, "import.state=?")
		arguments = append(arguments, state)
	}
	if beforeID != "" {
		conditions = append(conditions, "(import.created_at_ms<? OR (import.created_at_ms=? AND import.id<?))")
		arguments = append(arguments, beforeAt, beforeAt, beforeID)
	}
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(
		ctx,
		summaryQuery+` WHERE `+strings.Join(
			conditions,
			" AND ",
		)+` ORDER BY import.created_at_ms DESC,import.id DESC LIMIT ?`,
		arguments...)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/list: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Summary, 0)
	for rows.Next() {
		value, scanErr := scanSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate summaries: %w", err)
	}
	return result, nil
}

func (service *Service) Collections(
	ctx context.Context,
	importID, afterPath string,
	afterOrdinal int64,
	afterID string,
	limit int,
) ([]Collection, error) {
	if limit < 1 || limit > 101 {
		return nil, ErrInvalid
	}
	arguments := []any{importID}
	watermark := ""
	if afterID != "" {
		watermark = ` AND (collection.metadata_relative_path>? OR
(collection.metadata_relative_path=? AND collection.segment_ordinal>?) OR
(collection.metadata_relative_path=? AND collection.segment_ordinal=? AND collection.id>?))`
		arguments = append(arguments, afterPath, afterPath, afterOrdinal, afterPath, afterOrdinal, afterID)
	}
	arguments = append(arguments, limit)
	rows, err := service.database.QueryContext(ctx, `
SELECT collection.id,collection.metadata_relative_path,collection.segment_ordinal,collection.name,
collection.shortname,collection.description,collection.game_count,collection.issue_count,collection.mapping_action,
collection.target_platform_instance_id,platform.name,collection.target_default_core_id,core.name,
collection.ignored_rules_json,collection.warning_fields_json
FROM pegasus_import_collections collection
LEFT JOIN platform_instances platform ON platform.id=collection.target_platform_instance_id
LEFT JOIN cores core ON core.id=collection.target_default_core_id
WHERE collection.import_id=?`+watermark+`
ORDER BY collection.metadata_relative_path,collection.segment_ordinal,collection.id LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/list collections: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Collection, 0)
	for rows.Next() {
		var value Collection
		var shortName, action, platformID, platformName, coreID, coreName sql.NullString
		var ignored, warnings string
		if err := rows.Scan(&value.ID, &value.MetadataRelativePath, &value.SegmentOrdinal, &value.Name, &shortName,
			&value.Description, &value.GameCount, &value.IssueCount, &action, &platformID, &platformName, &coreID,
			&coreName, &ignored, &warnings); err != nil {
			return nil, fmt.Errorf("pegasusimport/scan collection: %w", err)
		}
		value.ShortName, value.MappingAction = nullableString(shortName), nullableString(action)
		value.TargetPlatformInstanceID, value.TargetPlatformInstanceName = nullableString(
			platformID,
		), nullableString(
			platformName,
		)
		value.TargetDefaultCoreID, value.TargetDefaultCoreName = nullableString(coreID), nullableString(coreName)
		value.IgnoredRules, value.WarningFields = jsonStrings(ignored), jsonStrings(warnings)
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate collections: %w", err)
	}
	return result, nil
}

func (service *Service) Items(
	ctx context.Context,
	importID, query, outcome, warning, collectionID, afterTitle, afterID string,
	limit int,
) ([]Item, error) {
	if limit < 1 || limit > 51 {
		return nil, ErrInvalid
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT item.id,item.title,item.collection_id,collection.name,
collection.target_platform_instance_id,platform.name,
item.metadata_relative_path,item.execution_state,item.content_kind,
item.warnings_json,item.discovery_code,item.error_code,item.retryable,
item.published_game_id,item.existing_game_id,item.existing_matches_json,item.updated_at_ms,
EXISTS(SELECT 1 FROM pegasus_import_item_assets asset WHERE asset.item_id=item.id AND asset.kind='COVER'),
EXISTS(SELECT 1 FROM pegasus_import_item_assets asset WHERE asset.item_id=item.id AND asset.kind='VIDEO')
FROM pegasus_import_items item
LEFT JOIN pegasus_import_collections collection ON collection.id=item.collection_id
LEFT JOIN platform_instances platform ON platform.id=collection.target_platform_instance_id
WHERE item.import_id=?
AND (?='' OR instr(lower(item.title),lower(?))>0)
AND (?='' OR item.execution_state=?)
AND (?='' OR EXISTS(
  SELECT 1
  FROM json_each(item.warnings_json) warning_value
  WHERE json_extract(warning_value.value,'$.code')=?
))
AND (?='' OR item.collection_id=?)
AND (?='' OR item.title>? OR (item.title=? AND item.id>?))
ORDER BY item.title,item.id
LIMIT ?`,
		importID,
		query, query,
		outcome, outcome,
		warning, warning,
		collectionID, collectionID,
		afterID, afterTitle, afterTitle, afterID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/list items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Item, 0)
	for rows.Next() {
		value, scanErr := scanItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate items: %w", err)
	}
	return result, nil
}

func scanItem(row rowScanner) (Item, error) {
	var value Item
	var collection, collectionName, target, targetName, kind sql.NullString
	var discovery, itemError, published, existing sql.NullString
	var warnings, existingMatches string
	var retryable, hasCover, hasVideo int
	if err := row.Scan(
		&value.ID, &value.Title, &collection, &collectionName, &target, &targetName,
		&value.MetadataRelativePath, &value.ExecutionState, &kind, &warnings, &discovery, &itemError,
		&retryable, &published, &existing, &existingMatches, &value.UpdatedAtMS, &hasCover, &hasVideo,
	); err != nil {
		return Item{}, fmt.Errorf("pegasusimport/scan item: %w", err)
	}
	value.CollectionID, value.CollectionName = nullableString(collection), nullableString(collectionName)
	value.TargetPlatformInstanceID = nullableString(target)
	value.TargetPlatformInstanceName = nullableString(targetName)
	value.ContentKind, value.DiscoveryCode = nullableString(kind), nullableString(discovery)
	value.ErrorCode = nullableString(itemError)
	value.PublishedGameID, value.ExistingGameID = nullableString(published), nullableString(existing)
	value.Retryable = retryable == 1
	_ = json.Unmarshal([]byte(warnings), &value.Warnings)
	_ = json.Unmarshal([]byte(existingMatches), &value.ExistingMatches)
	value.Media = ItemMedia{
		Cover: mediaProjection(hasCover == 1, value.Warnings, "cover"),
		Video: mediaProjection(hasVideo == 1, value.Warnings, "video"),
	}
	return value, nil
}

func mediaProjection(present bool, warnings []map[string]any, field string) string {
	for _, warning := range warnings {
		warningField, _ := warning["field"].(string)
		code, _ := warning["code"].(string)
		mediaWarning := strings.HasPrefix(code, "PEGASUS_IMAGE_") ||
			strings.HasPrefix(code, "PEGASUS_VIDEO_") ||
			strings.HasPrefix(code, "PEGASUS_MEDIA_")
		if warningField == field && mediaWarning {
			return "WARNING"
		}
	}
	if present {
		return "READY"
	}
	return "MISSING"
}

func (service *Service) UpdateMappings(
	ctx context.Context,
	importID string,
	expectedVersion int64,
	mappings []Mapping,
) (Summary, error) {
	if len(mappings) < 1 || len(mappings) > 100 {
		return Summary{}, ErrInvalid
	}
	seen := map[string]struct{}{}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/mapping transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version FROM pegasus_imports WHERE id=?`, importID,
	).Scan(&state, &version); err != nil {
		return Summary{}, ErrNotFound
	}
	if state != "AWAITING_MAPPING" || version != expectedVersion {
		return Summary{}, ErrMapping
	}
	now := service.now().UnixMilli()
	for _, mapping := range mappings {
		if mapping.CollectionID == "" || mappingAlreadySeen(seen, mapping.CollectionID) {
			return Summary{}, ErrInvalid
		}
		seen[mapping.CollectionID] = struct{}{}
		if err := applyMapping(ctx, transaction, importID, mapping, now); err != nil {
			return Summary{}, ErrInvalid
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports SET
mapped_collection_count=(SELECT count(*) FROM pegasus_import_collections WHERE import_id=? AND mapping_action='IMPORT'),
skipped_collection_count=(SELECT count(*) FROM pegasus_import_collections WHERE import_id=? AND mapping_action='SKIP'),
mapping_version=mapping_version+1,version=version+1,updated_at_ms=?
WHERE id=?`, importID, importID, now, importID); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/update mapping counts: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/commit mappings: %w", err)
	}
	return service.Get(ctx, importID)
}

func mappingAlreadySeen(seen map[string]struct{}, collectionID string) bool {
	_, exists := seen[collectionID]
	return exists
}

func applyMapping(
	ctx context.Context,
	transaction *sql.Tx,
	importID string,
	mapping Mapping,
	now int64,
) error {
	switch mapping.Action {
	case "SKIP":
		return skipCollection(ctx, transaction, importID, mapping, now)
	case "IMPORT":
		return importCollection(ctx, transaction, importID, mapping, now)
	default:
		return ErrInvalid
	}
}

func skipCollection(
	ctx context.Context,
	transaction *sql.Tx,
	importID string,
	mapping Mapping,
	now int64,
) error {
	if mapping.PlatformInstanceID != "" {
		return ErrInvalid
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_collections
SET mapping_action='SKIP',
target_platform_instance_id=NULL,
target_platform_instance_version=NULL,
target_platform_id=NULL,
target_default_core_id=NULL,
target_core_artifact_id=NULL,
target_core_artifact_version=NULL,
target_dat_version_id=NULL,
updated_at_ms=?
WHERE id=?
AND import_id=?`, now, mapping.CollectionID, importID)
	if err != nil || rowsAffected(result) != 1 {
		return ErrInvalid
	}
	return nil
}

func importCollection(
	ctx context.Context,
	transaction *sql.Tx,
	importID string,
	mapping Mapping,
	now int64,
) error {
	var instanceVersion, artifactVersion int64
	var platformID, coreID, artifactID string
	var datID sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT instance.version,
instance.platform_id,
instance.default_core_id,
artifact.id,
artifact.version,
(SELECT id FROM dat_versions WHERE core_artifact_id=artifact.id AND is_active=1)
FROM platform_instances instance
JOIN platforms platform ON platform.id=instance.platform_id AND platform.enabled=1
JOIN cores core ON core.id=instance.default_core_id AND core.enabled=1
JOIN core_artifacts artifact ON artifact.core_id=instance.default_core_id AND artifact.enabled=1
WHERE instance.id=?
AND instance.enabled=1
AND instance.deleted_at_ms IS NULL`, mapping.PlatformInstanceID).
		Scan(&instanceVersion, &platformID, &coreID, &artifactID, &artifactVersion, &datID)
	if err != nil {
		return ErrInvalid
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_collections
SET mapping_action='IMPORT',
target_platform_instance_id=?,
target_platform_instance_version=?,
target_platform_id=?,
target_default_core_id=?,
target_core_artifact_id=?,
target_core_artifact_version=?,
target_dat_version_id=?,
updated_at_ms=?
WHERE id=?
AND import_id=?`, mapping.PlatformInstanceID, instanceVersion, platformID, coreID, artifactID,
		artifactVersion, nullable(datID), now, mapping.CollectionID, importID)
	if err != nil || rowsAffected(result) != 1 {
		return ErrInvalid
	}
	return nil
}

func rowsAffected(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	value, _ := result.RowsAffected()
	return value
}

func nullable(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func (service *Service) verifySnapshot(ctx context.Context, importID, selectedPath string, root Root) error {
	rows, err := service.database.QueryContext(
		ctx,
		`SELECT relative_path,size_bytes,content_digest,source_facts_digest
FROM pegasus_import_metadata_files
WHERE import_id=?
ORDER BY relative_path`,
		importID,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/read metadata evidence: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	count := 0
	for rows.Next() {
		var path, expectedDigest, expectedFacts string
		var size int64
		if err := rows.Scan(&path, &size, &expectedDigest, &expectedFacts); err != nil {
			return ErrSourceChanged
		}
		file, before, err := serversource.OpenRelativeFile(root.path, selectedPath, path)
		if err != nil || before.Size() != size || serversource.FactsDigest(before) != expectedFacts {
			if file != nil {
				cleanup.Error("close", file.Close())
			}
			return ErrSourceChanged
		}
		hash := sha256.New()
		if _, err := file.WriteTo(hash); err != nil {
			cleanup.Error("close", file.Close())
			return ErrSourceChanged
		}
		after, err := file.Stat()
		cleanup.Error("close", file.Close())
		if err != nil || !serversource.SameFileFacts(before, after) ||
			hex.EncodeToString(hash.Sum(nil)) != expectedDigest {
			return ErrSourceChanged
		}
		count++
	}
	if err := rows.Err(); err != nil || count == 0 {
		return ErrSourceChanged
	}
	return nil
}

func (service *Service) StartImport(ctx context.Context, importID string, expectedVersion int64) (Summary, error) {
	summary, err := service.Get(ctx, importID)
	if err != nil {
		return Summary{}, err
	}
	if summary.State == "QUEUED" || summary.State == "RUNNING" || summary.State == "COMPLETED" ||
		summary.State == "PARTIAL_FAILURE" {
		return summary, nil
	}
	if summary.State != "AWAITING_MAPPING" || summary.Version != expectedVersion ||
		service.now().UnixMilli() >= summary.ExpiresAtMS {
		return Summary{}, ErrExpired
	}
	if summary.Counts.MappedCollections+summary.Counts.SkippedCollections != summary.Counts.Collections {
		return Summary{}, ErrMapping
	}
	if summary.Counts.MappedCollections == 0 {
		return Summary{}, ErrNoSelection
	}
	root, ok := service.roots[summary.Root.ID]
	if !ok {
		return Summary{}, ErrSourceChanged
	}
	if err := service.verifySnapshot(ctx, importID, summary.SourceRelativePath, root); err != nil {
		return Summary{}, err
	}
	if err := service.queueImport(ctx, summary, root, expectedVersion); err != nil {
		return Summary{}, err
	}
	service.signal()
	return service.Get(ctx, importID)
}

func (service *Service) queueImport(
	ctx context.Context,
	summary Summary,
	root Root,
	expectedVersion int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version FROM pegasus_imports WHERE id=?`, summary.ID,
	).Scan(&state, &version); err != nil ||
		state != "AWAITING_MAPPING" ||
		version != expectedVersion {
		return ErrMapping
	}
	jobID, _ := uuid.NewV7()
	executionID, _ := uuid.NewV7()
	now := service.now().UnixMilli()
	input := queuedImportInput(summary, root, expectedVersion, executionID.String())
	encoded, _ := json.Marshal(input)
	if err := createQueuedImportJob(ctx, transaction, summary.ID, jobID.String(), encoded, now); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state='SKIPPED_MAPPING',completed_at_ms=?,updated_at_ms=?
WHERE import_id=?
AND collection_id IN (
  SELECT id FROM pegasus_import_collections WHERE import_id=? AND mapping_action='SKIP'
)`, now, now, summary.ID, summary.ID); err != nil {
		return fmt.Errorf("pegasusimport/skip mapped items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state=CASE discovery_state
  WHEN 'BLOCKED_SOURCE' THEN 'BLOCKED_SOURCE'
  ELSE 'BLOCKED_CONTENT'
END,
completed_at_ms=?,updated_at_ms=?
WHERE import_id=?
AND execution_state='PENDING'
AND discovery_state!='READY'`, now, now, summary.ID); err != nil {
		return fmt.Errorf("pegasusimport/close discovery items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET import_job_id=?,state='QUEUED',phase=NULL,
blocked_item_count=(
  SELECT count(*)
  FROM pegasus_import_items
  WHERE import_id=? AND execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT')
),
version=version+1,updated_at_ms=?
WHERE id=?`, jobID.String(), summary.ID, now, summary.ID); err != nil {
		return fmt.Errorf("pegasusimport/queue import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(
?,'PEGASUS_IMPORT',?,'QUEUED',
'{"schemaVersion":1,"executionNo":1,"attempt":0}',?
)`, jobID.String(), summary.ID, now); err != nil {
		return fmt.Errorf("pegasusimport/queue event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit start: %w", err)
	}
	return nil
}

func queuedImportInput(summary Summary, root Root, expectedVersion int64, executionID string) map[string]any {
	return map[string]any{
		"schemaVersion": 1,
		"kind":          "SERVER_PEGASUS_IMPORT",
		"scope":         map[string]any{"type": "PEGASUS_IMPORT", "id": summary.ID},
		"executionId":   executionID,
		"inputs": map[string]any{
			"rootId":                summary.Root.ID,
			"sourceRelativePath":    summary.SourceRelativePath,
			"rootConfigDigest":      root.digest,
			"sourceSnapshotVersion": expectedVersion,
		},
	}
}

func createQueuedImportJob(
	ctx context.Context,
	transaction *sql.Tx,
	importID, jobID string,
	encoded []byte,
	now int64,
) error {
	digest := sha256.Sum256(encoded)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(
id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,
state,attempt_count,max_attempts,version,available_at_ms,created_at_ms,updated_at_ms
) VALUES(
?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_IMPORT',?,1,
'{"inputExecutionNo":1}',1,'QUEUED',0,4,1,?,?,?
)`, jobID, importID, jobDedupe("SERVER_PEGASUS_IMPORT", importID), now, now, now); err != nil {
		return fmt.Errorf("pegasusimport/create import job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,1,?,?,?)`, jobID, string(encoded), hex.EncodeToString(digest[:]), now); err != nil {
		return fmt.Errorf("pegasusimport/create import input: %w", err)
	}
	return nil
}

func (service *Service) Cancel(
	ctx context.Context,
	importID string,
	version int64,
	reason, userID string,
) (Summary, bool, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return Summary{}, false, ErrNotCancellable
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, false, fmt.Errorf("pegasusimport/cancel transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var actual int64
	var jobID sql.NullString
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version,import_job_id FROM pegasus_imports WHERE id=?`, importID,
	).Scan(&state, &actual, &jobID); err != nil ||
		actual != version ||
		!jobID.Valid ||
		state != "QUEUED" && state != "RUNNING" {
		return Summary{}, false, ErrNotCancellable
	}
	now := service.now().UnixMilli()
	pending := state == "RUNNING"
	if err := persistCancellation(ctx, transaction, cancellation{
		ImportID: importID,
		JobID:    jobID.String,
		Reason:   reason,
		UserID:   userID,
		Now:      now,
		Pending:  pending,
	}); err != nil {
		return Summary{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, false, fmt.Errorf("pegasusimport/commit cancel: %w", err)
	}
	result, err := service.Get(ctx, importID)
	return result, pending, err
}

type cancellation struct {
	ImportID string
	JobID    string
	Reason   string
	UserID   string
	Now      int64
	Pending  bool
}

func persistCancellation(ctx context.Context, transaction *sql.Tx, value cancellation) error {
	newState, jobState := "CANCELLED", "CANCELLED"
	var completed any = value.Now
	if value.Pending {
		newState, jobState, completed = "CANCEL_REQUESTED", "CANCEL_REQUESTED", nil
	} else if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state='CANCELLED',error_code='CANCELLED',completed_at_ms=?,updated_at_ms=?
WHERE import_id=? AND execution_state='PENDING'`, value.Now, value.Now, value.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/cancel pending items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state=?,cancel_requested_at_ms=?,cancel_reason=?,finished_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=?`, jobState, value.Now, value.Reason, completed, value.Now, value.JobID); err != nil {
		return fmt.Errorf("pegasusimport/cancel job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state=?,cancel_reason=?,
cancelled_item_count=(
  SELECT count(*)
  FROM pegasus_import_items
  WHERE import_id=? AND execution_state='CANCELLED'
),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=?`, newState, value.Reason, value.ImportID, completed, value.Now, value.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/cancel import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'CANCEL_REQUESTED','{"schemaVersion":1}',?)`,
		value.JobID, value.ImportID, value.Now); err != nil {
		return fmt.Errorf("pegasusimport/create cancel event: %w", err)
	}
	auditID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(
id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms
) VALUES(
?,'USER',?,NULL,'PEGASUS_IMPORT_CANCEL_REQUESTED','PEGASUS_IMPORT',
?,'{}','{}',NULL,NULL,?
)`, auditID.String(), value.UserID, value.ImportID, value.Now); err != nil {
		return fmt.Errorf("pegasusimport/create cancel audit: %w", err)
	}
	return nil
}

func (service *Service) Delete(ctx context.Context, importID string, expectedVersion int64) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/delete transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state, scanJob string
	var version int64
	var importJob sql.NullString
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,scan_job_id,import_job_id,version FROM pegasus_imports WHERE id=?`, importID,
	).Scan(&state, &scanJob, &importJob, &version); err != nil {
		return ErrNotFound
	}
	if version != expectedVersion || state != "AWAITING_MAPPING" && state != "EXPIRED" || importJob.Valid {
		return ErrInvalid
	}
	for _, statement := range []string{
		`DELETE FROM pegasus_import_item_assets WHERE item_id IN (SELECT id FROM pegasus_import_items WHERE import_id=?)`,
		`DELETE FROM pegasus_import_item_files WHERE item_id IN (SELECT id FROM pegasus_import_items WHERE import_id=?)`,
		`DELETE FROM pegasus_import_items WHERE import_id=?`,
		`DELETE FROM pegasus_import_collections WHERE import_id=?`,
		`DELETE FROM pegasus_import_metadata_files WHERE import_id=?`,
		`DELETE FROM pegasus_imports WHERE id=?`,
	} {
		if _, err := transaction.ExecContext(ctx, statement, importID); err != nil {
			return fmt.Errorf("pegasusimport/delete plan: %w", err)
		}
	}
	// Immutable job/input/event evidence intentionally remains after the plan's
	// mutable scan projection is removed.
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit delete: %w", err)
	}
	return nil
}

func (service *Service) ExpirePlans(ctx context.Context) error {
	now := service.now().UnixMilli()
	_, err := service.database.ExecContext(
		ctx,
		`UPDATE pegasus_imports
SET state='EXPIRED',phase=NULL,last_error_code='PEGASUS_PLAN_EXPIRED',
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state='AWAITING_MAPPING' AND expires_at_ms<=?`,
		now,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/expire plans: %w", err)
	}
	return nil
}

func (service *Service) Retry(ctx context.Context, importID string, version int64, userID string) (Summary, error) {
	_ = userID
	summary, err := service.Get(ctx, importID)
	if err != nil || summary.Version != version || !summary.Retryable || summary.ImportJobID == nil ||
		summary.State != "FAILED" && summary.State != "PARTIAL_FAILURE" {
		return Summary{}, ErrNotRetryable
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/retry transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var execution int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT execution_no FROM jobs WHERE id=?`, *summary.ImportJobID,
	).Scan(&execution); err != nil {
		return Summary{}, ErrNotRetryable
	}
	execution++
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state='PENDING',error_code=NULL,retryable=0,
completed_at_ms=NULL,updated_at_ms=?
WHERE import_id=?
AND retryable=1
AND execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')`, now, importID); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/reset retryable items: %w", err)
	}
	inputID, _ := uuid.NewV7()
	input := map[string]any{
		"schemaVersion": 1,
		"kind":          "SERVER_PEGASUS_IMPORT",
		"scope":         map[string]any{"type": "PEGASUS_IMPORT", "id": importID},
		"executionId":   inputID.String(),
		"inputs":        map[string]any{"retry": true, "version": version},
	}
	encoded, _ := json.Marshal(input)
	digest := sha256.Sum256(encoded)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES(?,?,?,?,?)`, *summary.ImportJobID, execution, string(encoded), hex.EncodeToString(digest[:]), now); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create retry input: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='QUEUED',execution_no=?,payload_json=json_object('inputExecutionNo',?),
attempt_count=0,available_at_ms=?,execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,leased_until_ms=NULL,heartbeat_at_ms=NULL,
finished_at_ms=NULL,worker_id=NULL,error_code=NULL,error_retryable=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE id=?`, execution, execution, now, now, *summary.ImportJobID); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/queue retry job: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET state='QUEUED',phase=NULL,last_error_code=NULL,retryable=0,
completed_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=?`, now, importID); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/queue retry import: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(
?,'PEGASUS_IMPORT',?,'MANUAL_RETRY',
json_object('schemaVersion',1,'executionNo',?),?
)`, *summary.ImportJobID, importID, execution, now); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/create retry event: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/commit retry: %w", err)
	}
	service.signal()
	return service.Get(ctx, importID)
}

// Keep the imported time package tied to the seven-day contract in this file.
var _ = 7 * 24 * time.Hour
