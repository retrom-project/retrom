package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/capability/content/multidisc"
	"retrom/internal/foundation/cleanup"
)

func (records multidiscAdmissionRecords) Entries(ctx context.Context, snapshotID string) ([]multidisc.Entry, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT ordinal,source_reference,normalized_reference,canonical_name,state,
upload_file_id,blob_id,source_logical_name
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
		var uploadFileID, blobID, logicalName sql.NullString
		if err := rows.Scan(
			&entry.Ordinal, &entry.SourceReference, &entry.NormalizedReference,
			&entry.CanonicalName, &state, &uploadFileID, &blobID, &logicalName,
		); err != nil {
			return nil, fmt.Errorf("scan multi-disc entries: %w", err)
		}
		entry.State = multidisc.EntryState(state)
		if entry.State == multidisc.EntryPresent && uploadFileID.Valid && blobID.Valid && logicalName.Valid {
			entry.File = &multidisc.File{
				UploadFileID: uploadFileID.String, BlobID: blobID.String, LogicalName: logicalName.String,
			}
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate multi-disc entries: %w", err)
	}
	return entries, nil
}
