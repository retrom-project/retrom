package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/multidisc"
)

func (records multidiscAdmissionRecords) Entries(ctx context.Context, snapshotID string) ([]multidisc.Entry, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT ordinal,source_reference,normalized_reference,canonical_name,state,
upload_file_id,file_record,source_logical_name
FROM import_item_multidisc_entries
WHERE source_snapshot_id=? ORDER BY ordinal
`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read multi-disc entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]multidisc.Entry, 0, multidisc.MaxDiscs)
	for rows.Next() {
		var entry multidisc.Entry
		var state string
		var uploadFileID, fileRecord, logicalName sql.NullString
		if err := rows.Scan(
			&entry.Ordinal, &entry.SourceReference, &entry.NormalizedReference,
			&entry.CanonicalName, &state, &uploadFileID, &fileRecord, &logicalName,
		); err != nil {
			return nil, fmt.Errorf("scan multi-disc entries: %w", err)
		}
		entry.State = multidisc.EntryState(state)
		if entry.State == multidisc.EntryPresent && uploadFileID.Valid && fileRecord.Valid && logicalName.Valid {
			entry.File = &multidisc.File{
				UploadFileID: uploadFileID.String, FileRecord: fileRecord.String, LogicalName: logicalName.String,
			}
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multi-disc entries: %w", err)
	}
	return entries, nil
}
