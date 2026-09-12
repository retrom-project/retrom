package pegasusimport

import (
	"context"
	"fmt"

	application "retrom/internal/service/pegasusimport"
)

func (records scanRecords) Headers(
	ctx context.Context,
	owner application.ScanLease,
	headers application.ScanHeaders,
) error {
	if len(headers.Metadata)+len(headers.Collections) > 500 {
		return application.ErrScanLimit
	}
	if err := records.guard(ctx, owner); err != nil {
		return err
	}
	for _, metadata := range headers.Metadata {
		result, err := records.tx.ExecContext(ctx, `INSERT INTO pegasus_import_metadata_files(
import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,error_code,created_at_ms
) VALUES(?,?,?,?,?,?,?,?)`, owner.Before.ImportID, metadata.Path, metadata.Size, metadata.Digest,
			metadata.Facts, metadata.State, optionalText(metadata.ErrorCode), owner.NowMS)
		if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
			return fmt.Errorf("insert Pegasus scan metadata evidence: %w", err)
		}
	}
	for _, collection := range headers.Collections {
		result, err := records.tx.ExecContext(ctx, `INSERT INTO pegasus_import_collections(
id,import_id,metadata_relative_path,segment_ordinal,name,shortname,description,game_count,issue_count,
ignored_rules_json,warning_fields_json,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			collection.ID, owner.Before.ImportID, collection.MetadataPath, collection.SegmentOrdinal, collection.Name,
			collection.ShortName, collection.Description, collection.GameCount, collection.IssueCount, collection.IgnoredJSON,
			collection.WarningJSON, owner.NowMS, owner.NowMS)
		if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
			return fmt.Errorf("insert Pegasus scan collection: %w", err)
		}
	}
	return nil
}
