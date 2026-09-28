package libraryimport

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records reviewApprovalRecords) PublicationFiles(ctx context.Context,
	head libraryservice.ReviewApprovalHead,
) ([]string, error) {
	values, err := dbapi.QueryStrings(ctx, records.transaction, `
SELECT file_record FROM import_item_source_snapshot_files WHERE source_snapshot_id=?
 UNION SELECT file_record FROM import_item_validation_files WHERE import_item_core_validation_id=? AND
role<>'BIOS_BUNDLE'`, head.SourceSnapshotID, head.ValidationID)
	if err != nil {
		return nil, fmt.Errorf("read publication files: %w", err)
	}
	return values, nil
}

func (records reviewApprovalRecords) PreparePublicationRecords(ctx context.Context,
	intent libraryservice.Publication,
) error {
	for _, file := range intent.Files {
		if err := recordstore.CopyArchiveFacts(ctx, records.transaction, file.Source, file.Staged); err != nil {
			return fmt.Errorf("prepare publication records: %w", err)
		}
		if _, err := records.transaction.ExecContext(ctx, `
UPDATE import_item_source_snapshot_files SET file_record=? WHERE source_snapshot_id=? AND file_record=?
`, file.Staged, intent.Head.SourceSnapshotID, file.Source); err != nil {
			return fmt.Errorf("prepare publication records: %w", err)
		}
		if _, err := records.transaction.ExecContext(ctx, `
UPDATE import_item_validation_files SET file_record=? WHERE import_item_core_validation_id=? AND file_record=?
`, file.Staged, intent.Head.ValidationID, file.Source); err != nil {
			return fmt.Errorf("prepare publication records: %w", err)
		}
	}
	values, err := records.PublicationFiles(ctx, intent.Head)
	if err != nil {
		return err
	}
	for _, value := range values {
		published, err := filestore.PublishedRecord(value, intent.Request.ItemID, intent.GameID)
		if err != nil {
			return fmt.Errorf("prepare publication records: %w", err)
		}
		if err := recordstore.CopyArchiveFacts(ctx, records.transaction, value, published); err != nil {
			return fmt.Errorf("prepare publication records: %w", err)
		}
	}
	return nil
}
