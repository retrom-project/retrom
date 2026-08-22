package libraryimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/contentmanifest"
)

type groupRecord struct {
	group             *preparedGroup
	itemID            string
	sourceSnapshotID  string
	validationID      string
	draftID           string
	contentKind       string
	manifestJSON      []byte
	manifestDigest    string
	searchParts       []string
	uploadFileIDs     map[string]struct{}
	validationStatus  string
	compatibilityCode string
}

func (run *creationRun) newGroupRecord(group *preparedGroup) (*groupRecord, error) {
	itemID, _ := uuid.NewV7()
	sourceSnapshotID, _ := uuid.NewV7()
	validationID, _ := uuid.NewV7()
	draftID, _ := uuid.NewV7()
	manifestFiles, err := run.buildManifestFiles(group.sources)
	if err != nil {
		return nil, err
	}
	manifestJSON, manifestDigest, err := contentmanifest.Build(manifestFiles)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	return &groupRecord{
		group: group, itemID: itemID.String(), sourceSnapshotID: sourceSnapshotID.String(),
		validationID: validationID.String(), draftID: draftID.String(),
		contentKind: preparedGroupContentKind(*group), manifestJSON: manifestJSON,
		manifestDigest: manifestDigest, uploadFileIDs: make(map[string]struct{}),
	}, nil
}

func (run *creationRun) buildManifestFiles(sources []preparedSource) ([]contentmanifest.File, error) {
	files := make([]contentmanifest.File, 0, len(sources))
	for _, source := range sources {
		blobID := run.sourceBlobID(source)
		blobSHA, blobSize, err := run.blobIdentity(blobID)
		if err != nil {
			return nil, err
		}
		archiveSHAValue, hasArchive, err := run.sourceArchiveSHA(source)
		if err != nil {
			return nil, err
		}
		var archiveSHA *string
		if hasArchive {
			archiveSHA = &archiveSHAValue
		}
		files = append(files, contentmanifest.File{
			Role: source.role, LogicalName: source.logicalName, BlobSHA256: blobSHA, SizeBytes: blobSize,
			SourceArchiveSHA256: archiveSHA, SourceArchiveEntryOrdinal: source.archiveOrdinal,
		})
	}
	return files, nil
}

func (run *creationRun) sourceBlobID(source preparedSource) string {
	if source.archiveOrdinal == nil {
		return source.file.blobID
	}
	return run.materialized[fmt.Sprintf("%s:%d", source.archiveBlobID, *source.archiveOrdinal)]
}

func (run *creationRun) blobIdentity(blobID string) (string, int64, error) {
	var sha string
	var size int64
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT sha256,size_bytes FROM blobs WHERE id=?
`, blobID).Scan(&sha, &size)
	if err != nil {
		return "", 0, fmt.Errorf("libraryimport/service: %w", err)
	}
	return sha, size, nil
}

func (run *creationRun) sourceArchiveSHA(source preparedSource) (string, bool, error) {
	if source.archiveOrdinal == nil {
		return "", false, nil
	}
	var value string
	err := run.transaction.QueryRowContext(run.ctx, `
SELECT sha256 FROM blobs WHERE id=?
`, source.archiveBlobID).Scan(&value)
	if err != nil {
		return "", false, fmt.Errorf("libraryimport/service: %w", err)
	}
	return value, true, nil
}

func (run *creationRun) persistGroupSource(record *groupRecord) error {
	groupKey, searchText := groupIdentity(record.group.sources)
	if record.group.groupKey != "" {
		groupKey = record.group.groupKey
	}
	record.searchParts = searchText
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_items(
  id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,?,?,?,?,?,?,1,?,?)
`, record.itemID, run.importID, groupKey, run.progress.itemState, string(record.manifestJSON),
		record.manifestDigest, strings.ToLower(strings.Join(searchText, " ")), run.now, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	if err := run.persistGroupSourceFiles(record); err != nil {
		return err
	}
	return run.persistSourceSnapshot(record)
}

func groupIdentity(sources []preparedSource) (string, []string) {
	identity := make([]map[string]any, 0, len(sources))
	searchParts := make([]string, 0, len(sources))
	for _, source := range sources {
		identity = append(identity, map[string]any{
			"relativePath": source.file.path, "sourceSha256": source.file.sha256,
			"role": source.role, "logicalName": source.logicalName,
			"archiveOrdinal": nullableIntPointer(source.archiveOrdinal),
		})
		searchParts = append(searchParts, source.file.path)
	}
	input, _ := json.Marshal(identity)
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:]), searchParts
}

func (run *creationRun) persistGroupSourceFiles(record *groupRecord) error {
	for index, source := range record.group.sources {
		sortOrder := index
		if source.sortOrder != nil {
			sortOrder = *source.sortOrder
		}
		_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_item_source_files(
  import_item_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,
  source_archive_entry_ordinal,sort_order,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, record.itemID, source.role, source.logicalName, source.file.id, run.sourceBlobID(source),
			nullableText(source.archiveBlobID), nullableIntPointer(source.archiveOrdinal), sortOrder, run.now)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
		record.uploadFileIDs[source.file.id] = struct{}{}
	}
	return nil
}

func (run *creationRun) persistSourceSnapshot(record *groupRecord) error {
	_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_item_source_snapshots(
  id,import_item_id,revision_no,content_kind,source_manifest_json,source_manifest_digest,created_by,created_at_ms
) VALUES(?,?,1,?,?,?,'IDENTIFICATION',?)
`, record.sourceSnapshotID, record.itemID, record.contentKind,
		string(record.manifestJSON), record.manifestDigest, run.now)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	_, err = run.transaction.ExecContext(run.ctx, `
INSERT INTO import_item_source_snapshot_files(
  source_snapshot_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,
  source_archive_entry_ordinal,sort_order,created_at_ms
)
SELECT ?,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,
  source_archive_entry_ordinal,sort_order,created_at_ms
FROM import_item_source_files WHERE import_item_id=?
`, record.sourceSnapshotID, record.itemID)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	return run.persistMultiDiscEntries(record)
}

func (run *creationRun) persistMultiDiscEntries(record *groupRecord) error {
	for _, entry := range record.group.multiEntries {
		_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_item_multidisc_entries(
  source_snapshot_id,ordinal,source_reference,normalized_reference,canonical_name,state,
  upload_file_id,blob_id,source_logical_name,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?)
`, record.sourceSnapshotID, entry.ordinal, entry.sourceReference, entry.normalizedReference,
			entry.canonicalName, entry.state, nullableText(entry.uploadFileID), nullableText(entry.blobID),
			nullableText(entry.sourceLogicalName), run.now)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	return nil
}
