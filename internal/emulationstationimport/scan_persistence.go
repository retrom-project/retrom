package emulationstationimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/emulationstationmeta"
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

func (service *Service) persistRejectedScan(ctx context.Context, unit work, result scanResult) error {
	now := service.now().UnixMilli()
	if err := service.persistScanHeaders(ctx, unit, result, now); err != nil {
		return err
	}
	if _, err := service.database.ExecContext(ctx, `
UPDATE emulationstation_imports
SET gamelist_count=?,invalid_gamelist_count=?,
collection_count=0,folder_entry_count=0,game_count=0,estimated_source_bytes=0,
processable_item_count=0,blocked_item_count=0,media_warning_count=0,
discovered_cover_count=0,discovered_video_count=0,version=version+1,updated_at_ms=?
WHERE id=? AND state='SCANNING'
`, len(result.Gamelists), result.InvalidGamelists, now, unit.ImportID); err != nil {
		return fmt.Errorf("emulationstationimport/persist rejected scan evidence: %w", err)
	}
	return nil
}

func (service *Service) persistScanHeaders(
	ctx context.Context,
	unit work,
	result scanResult,
	now int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("emulationstationimport/start scan header transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	for _, gamelist := range result.Gamelists {
		ignoredFields := gamelist.Document.IgnoredFields
		if ignoredFields == nil {
			ignoredFields = []string{}
		}
		ignoredJSON := string(compactJSON(ignoredFields))
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO emulationstation_import_gamelists(
import_id,relative_path,size_bytes,content_digest,source_facts_digest,
parse_state,error_code,game_count,folder_count,provider_present,
ignored_fields_json,ignored_field_other_count,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, unit.ImportID, gamelist.Path, gamelist.Size, nullIfEmpty(gamelist.Digest),
			gamelist.Facts, gamelist.State, nullIfEmpty(gamelist.ErrorCode),
			len(gamelist.Document.Games), gamelist.Document.FolderEntryCount,
			boolInt(gamelist.Document.ProviderPresent), ignoredJSON,
			gamelist.Document.IgnoredFieldOtherCount, now); err != nil {
			return fmt.Errorf("emulationstationimport/insert gamelist evidence: %w", err)
		}
	}
	for _, collection := range result.Collections {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO emulationstation_import_collections(
id,import_id,gamelist_relative_path,relative_directory,display_name,
game_count,issue_count,folder_entry_count,hidden_game_count,adult_game_count,
extension_summary_json,extension_other_count,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, collection.ID, unit.ImportID, collection.GamelistPath,
			collection.RelativeDirectory, collection.DisplayName, collection.GameCount, collection.IssueCount,
			collection.FolderEntryCount, collection.HiddenGameCount, collection.AdultGameCount,
			collection.ExtensionSummaryJSON, collection.ExtensionOtherCount, now, now); err != nil {
			return fmt.Errorf("emulationstationimport/insert collection: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("emulationstationimport/commit scan headers: %w", err)
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
			return fmt.Errorf("emulationstationimport/start item batch: %w", beginErr)
		}
		for _, item := range items[offset:end] {
			if err := insertScannedItem(ctx, batch, unit.ImportID, item, now); err != nil {
				cleanup.Rollback(batch)
				return err
			}
		}
		if err := batch.Commit(); err != nil {
			return fmt.Errorf("emulationstationimport/commit item batch: %w", err)
		}
	}
	return nil
}

func insertScannedItem(ctx context.Context, batch *sql.Tx, importID string, item scannedItem, now int64) error {
	if _, err := batch.ExecContext(ctx, `
INSERT INTO emulationstation_import_items(
id,import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,
source_flags_json,discovery_state,execution_state,content_kind,metadata_json,
warnings_json,source_manifest_json,source_manifest_digest,discovery_code,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,'PENDING',?,?,?,?,?,?,?,?)`, item.ID, importID,
		item.CollectionID, item.GamelistPath, item.GameOrdinal, item.SourceKey,
		item.Title, item.SourceFlagsJSON, item.DiscoveryState, item.ContentKind,
		item.MetadataJSON, item.WarningsJSON,
		item.SourceManifestJSON, item.SourceManifestDigest, nullIfEmpty(item.DiscoveryCode), now, now); err != nil {
		return fmt.Errorf("emulationstationimport/insert item: %w", err)
	}
	for _, file := range item.Files {
		if _, err := batch.ExecContext(ctx, `
INSERT INTO emulationstation_import_item_files(
item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,
state,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,'DISCOVERED',?,?)`, item.ID, file.Ordinal, file.Kind,
			file.Path, file.Size, file.Facts, now, now); err != nil {
			return fmt.Errorf("emulationstationimport/insert item file: %w", err)
		}
	}
	for _, asset := range item.Assets {
		if _, err := batch.ExecContext(ctx, `
INSERT INTO emulationstation_import_item_assets(
item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,
media_type,width_px,height_px,state,warning_code,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, asset.Kind, asset.Method,
			asset.Path, asset.Size, asset.Facts, asset.MediaType, asset.Width, asset.Height,
			asset.State, nullIfEmpty(asset.WarningCode), now, now); err != nil {
			return fmt.Errorf("emulationstationimport/insert asset: %w", err)
		}
	}
	return nil
}

func (service *Service) finishScan(ctx context.Context, unit work, result scanResult, now int64) error {
	finish, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("emulationstationimport/start scan finish transaction: %w", err)
	}
	defer cleanup.Rollback(finish)
	processable := int64(len(result.Items)) - result.Blocked
	if _, err := finish.ExecContext(ctx, `
UPDATE emulationstation_imports
SET source_snapshot_digest=?,state='AWAITING_MAPPING',phase=NULL,
gamelist_count=?,invalid_gamelist_count=?,collection_count=?,folder_entry_count=?,game_count=?,
estimated_source_bytes=?,processable_item_count=?,blocked_item_count=?,
media_warning_count=?,discovered_cover_count=?,discovered_video_count=?,
scan_completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='SCANNING'`, result.SnapshotDigest, len(result.Gamelists),
		result.InvalidGamelists, len(result.Collections), result.FolderEntries, len(result.Items), result.EstimatedBytes,
		processable, result.Blocked, result.MediaWarnings, result.Covers, result.Videos,
		now, now, unit.ImportID); err != nil {
		return fmt.Errorf("emulationstationimport/finish scan aggregate: %w", err)
	}
	if _, err := finish.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
error_code=NULL,error_retryable=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'`, now, now, unit.JobID); err != nil {
		return fmt.Errorf("emulationstationimport/finish scan job: %w", err)
	}
	data, _ := json.Marshal(
		map[string]any{
			"schemaVersion":    1,
			"gamelists":        len(result.Gamelists),
			"collections":      len(result.Collections),
			"games":            len(result.Items),
			"invalidGamelists": result.InvalidGamelists,
		},
	)
	if _, err := finish.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'SUCCEEDED',?,?)`,
		unit.JobID, unit.ImportID, string(data), now); err != nil {
		return fmt.Errorf("emulationstationimport/create scan success event: %w", err)
	}
	if err := finish.Commit(); err != nil {
		return fmt.Errorf("emulationstationimport/commit scan finish: %w", err)
	}
	return nil
}

func (service *Service) fail(ctx context.Context, unit work, code string, retryable bool) {
	now := service.now().UnixMilli()
	deadlineExpired := errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		(unit.DeadlineAtMS > 0 && unit.DeadlineAtMS <= now)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if deadlineExpired {
		code = "EMULATIONSTATION_EXECUTION_TIMEOUT"
		retryable = false
	}
	if retryable {
		outcome, err := service.scheduleAutomaticRetry(ctx, unit, code, now)
		if err == nil {
			switch outcome {
			case retryScheduled:
				return
			case retryDeadlineExhausted:
				code = "EMULATIONSTATION_EXECUTION_TIMEOUT"
				retryable = false
			case retryAttemptsExhausted:
				code = "EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED"
				retryable = false
			case retryNotEligible:
			}
		}
	}
	service.persistExecutionFailure(ctx, unit, code, retryable, now)
}

func parserErrorCode(err error) string {
	switch {
	case errors.Is(err, emulationstationmeta.ErrTooLarge):
		return emulationstationmeta.ErrTooLarge.Error()
	case errors.Is(err, emulationstationmeta.ErrInvalidUTF8):
		return emulationstationmeta.ErrInvalidUTF8.Error()
	case errors.Is(err, emulationstationmeta.ErrInvalidRoot):
		return emulationstationmeta.ErrInvalidRoot.Error()
	case errors.Is(err, emulationstationmeta.ErrLimitExceeded):
		return emulationstationmeta.ErrLimitExceeded.Error()
	default:
		return emulationstationmeta.ErrInvalidXML.Error()
	}
}

func (service *Service) clearScanStaging(ctx context.Context, importID string) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("emulationstationimport/start scan staging cleanup: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	if err := transaction.QueryRowContext(
		ctx, `SELECT state FROM emulationstation_imports WHERE id=?`, importID,
	).Scan(&state); err != nil || state != "SCANNING" {
		return ErrInvalid
	}
	for _, statement := range []string{
		`DELETE FROM emulationstation_import_item_assets WHERE item_id IN (
 SELECT id FROM emulationstation_import_items WHERE import_id=?)`,
		`DELETE FROM emulationstation_import_item_files WHERE item_id IN (
 SELECT id FROM emulationstation_import_items WHERE import_id=?)`,
		`DELETE FROM emulationstation_import_items WHERE import_id=?`,
		`DELETE FROM emulationstation_collection_tags WHERE collection_id IN (
 SELECT id FROM emulationstation_import_collections WHERE import_id=?)`,
		`DELETE FROM emulationstation_import_collections WHERE import_id=?`,
		`DELETE FROM emulationstation_import_gamelists WHERE import_id=?`,
	} {
		if _, err := transaction.ExecContext(ctx, statement, importID); err != nil {
			return fmt.Errorf("emulationstationimport/clear scan staging: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("emulationstationimport/commit scan staging cleanup: %w", err)
	}
	return nil
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
