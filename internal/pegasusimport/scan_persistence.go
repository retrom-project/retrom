package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"retrom/internal/cleanup"
	"retrom/internal/pegasusmeta"
)

func (service *Service) persistScan(ctx context.Context, unit work, result scanResult) error {
	now := service.now().UnixMilli()
	if err := service.persistScanHeaders(ctx, unit, result, now); err != nil {
		return err
	}
	if err := service.persistScanItems(ctx, unit, result.Items, now); err != nil {
		return err
	}
	return service.finishScan(ctx, unit, result, now)
}

func (service *Service) persistScanHeaders(
	ctx context.Context,
	unit work,
	result scanResult,
	now int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start scan header transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	for _, metadata := range result.Metadata {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO pegasus_import_metadata_files(
import_id,relative_path,size_bytes,content_digest,source_facts_digest,
parse_state,error_code,created_at_ms
) VALUES(?,?,?,?,?,?,?,?)`, unit.ImportID, metadata.Path, metadata.Size, metadata.Digest,
			metadata.Facts, metadata.State, nullIfEmpty(metadata.ErrorCode), now); err != nil {
			return fmt.Errorf("pegasusimport/insert metadata evidence: %w", err)
		}
	}
	for _, collection := range result.Collections {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO pegasus_import_collections(
id,import_id,metadata_relative_path,segment_ordinal,name,shortname,description,
game_count,issue_count,ignored_rules_json,warning_fields_json,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, collection.ID, unit.ImportID, collection.MetadataPath,
			collection.SegmentOrdinal, collection.Name, collection.ShortName, collection.Description,
			collection.GameCount, collection.IssueCount, collection.IgnoredJSON,
			collection.WarningJSON, now, now); err != nil {
			return fmt.Errorf("pegasusimport/insert collection: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit scan headers: %w", err)
	}
	return nil
}

func (service *Service) persistScanItems(
	ctx context.Context,
	unit work,
	items []scannedItem,
	now int64,
) error {
	for offset := 0; offset < len(items); offset += 500 {
		end := min(offset+500, len(items))
		batch, beginErr := service.database.BeginTx(ctx, nil)
		if beginErr != nil {
			return fmt.Errorf("pegasusimport/start item batch: %w", beginErr)
		}
		for _, item := range items[offset:end] {
			if err := insertScannedItem(ctx, batch, unit.ImportID, item, now); err != nil {
				cleanup.Rollback(batch)
				return err
			}
		}
		if err := batch.Commit(); err != nil {
			return fmt.Errorf("pegasusimport/commit item batch: %w", err)
		}
	}
	return nil
}

func insertScannedItem(ctx context.Context, batch *sql.Tx, importID string, item scannedItem, now int64) error {
	if _, err := batch.ExecContext(ctx, `
INSERT INTO pegasus_import_items(
id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,
discovery_state,execution_state,metadata_json,warnings_json,source_manifest_json,
source_manifest_digest,discovery_code,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,'PENDING',?,?,?,?,?,?,?)`, item.ID, importID,
		nullIfEmpty(item.CollectionID), item.MetadataPath, item.GameOrdinal, item.SourceKey,
		item.Title, item.DiscoveryState, item.MetadataJSON, item.WarningsJSON,
		item.SourceManifestJSON, item.SourceManifestDigest, nullIfEmpty(item.DiscoveryCode), now, now); err != nil {
		return fmt.Errorf("pegasusimport/insert item: %w", err)
	}
	for _, file := range item.Files {
		if _, err := batch.ExecContext(ctx, `
INSERT INTO pegasus_import_item_files(
item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,
state,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,'DISCOVERED',?,?)`, item.ID, file.Ordinal, file.Kind,
			file.Path, file.Size, file.Facts, now, now); err != nil {
			return fmt.Errorf("pegasusimport/insert item file: %w", err)
		}
	}
	for _, asset := range item.Assets {
		if _, err := batch.ExecContext(ctx, `
INSERT INTO pegasus_import_item_assets(
item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,
media_type,width_px,height_px,state,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,'DISCOVERED',?,?)`, item.ID, asset.Kind, asset.Method,
			asset.Path, asset.Size, asset.Facts, asset.MediaType, asset.Width, asset.Height, now, now); err != nil {
			return fmt.Errorf("pegasusimport/insert asset: %w", err)
		}
	}
	return nil
}

func (service *Service) finishScan(ctx context.Context, unit work, result scanResult, now int64) error {
	finish, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pegasusimport/start scan finish transaction: %w", err)
	}
	defer cleanup.Rollback(finish)
	processable := int64(len(result.Items)) - result.Blocked
	if _, err := finish.ExecContext(ctx, `
UPDATE pegasus_imports
SET source_snapshot_digest=?,state='AWAITING_MAPPING',phase=NULL,
metadata_count=?,invalid_metadata_count=?,collection_count=?,game_count=?,
estimated_source_bytes=?,processable_item_count=?,blocked_item_count=?,
media_warning_count=?,discovered_cover_count=?,discovered_video_count=?,
scan_completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='SCANNING'`, result.SnapshotDigest, len(result.Metadata),
		result.InvalidMetadata, len(result.Collections), len(result.Items), result.EstimatedBytes,
		processable, result.Blocked, result.MediaWarnings, result.Covers, result.Videos,
		now, now, unit.ImportID); err != nil {
		return fmt.Errorf("pegasusimport/finish scan aggregate: %w", err)
	}
	if _, err := finish.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'`, now, now, unit.JobID); err != nil {
		return fmt.Errorf("pegasusimport/finish scan job: %w", err)
	}
	data, _ := json.Marshal(
		map[string]any{
			"schemaVersion":   1,
			"metadata":        len(result.Metadata),
			"collections":     len(result.Collections),
			"games":           len(result.Items),
			"invalidMetadata": result.InvalidMetadata,
		},
	)
	if _, err := finish.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SUCCEEDED',?,?)`,
		unit.JobID, unit.ImportID, string(data), now); err != nil {
		return fmt.Errorf("pegasusimport/create scan success event: %w", err)
	}
	if err := finish.Commit(); err != nil {
		return fmt.Errorf("pegasusimport/commit scan finish: %w", err)
	}
	return nil
}

func (service *Service) fail(ctx context.Context, unit work, code string, retryable bool) {
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	_, _ = transaction.ExecContext(
		ctx,
		`UPDATE jobs
SET state='FAILED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=?,error_retryable=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'`,
		now,
		code,
		boolInt(retryable),
		now,
		unit.JobID,
	)
	_, _ = transaction.ExecContext(
		ctx,
		`UPDATE pegasus_imports
SET state='FAILED',phase=NULL,last_error_code=?,retryable=?,completed_at_ms=?,
version=version+1,updated_at_ms=?
WHERE id=?`,
		code,
		boolInt(retryable),
		now,
		now,
		unit.ImportID,
	)
	data, _ := json.Marshal(map[string]any{"schemaVersion": 1, "code": code, "retryable": retryable})
	_, _ = transaction.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'FAILED',?,?)`,
		unit.JobID,
		unit.ImportID,
		string(data),
		now,
	)
	_ = transaction.Commit()
}

func parserErrorCode(err error) string {
	switch {
	case errors.Is(err, pegasusmeta.ErrTooLarge):
		return pegasusmeta.ErrTooLarge.Error()
	case errors.Is(err, pegasusmeta.ErrInvalidUTF8):
		return pegasusmeta.ErrInvalidUTF8.Error()
	default:
		return pegasusmeta.ErrSyntax.Error()
	}
}

func asciiFold(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		return character
	}, value)
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func int64Pointer(value int64) *int64 { return &value }

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var _ = sql.ErrNoRows
