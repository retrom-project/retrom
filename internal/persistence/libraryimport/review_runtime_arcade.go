package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/content/arcade"
	arcaderecords "retrom/internal/persistence/arcade"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/importing"
	service "retrom/internal/service/libraryimport"
)

func readArcadeRuntime(ctx context.Context, executor dbapi.Executor, result ReviewRuntime) (ReviewRuntime, error) {
	machine := observedArcadeMachine(result.DependencyJSON)
	if result.DATID == nil {
		result.Status, result.Code = "INCOMPATIBLE", "ARCADE_DAT_UNAVAILABLE"
		return result, nil
	}
	archives, err := readReviewArcadeArchives(ctx, executor, result.SnapshotID)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("read arcade facts: %w", err)
	}
	current, err := arcade.Resolve(ctx, arcaderecords.New(executor), BindCreationArcade(executor),
		result.ProviderID, result.TargetID, *result.DATID, machine, archives)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("read arcade facts: %w", err)
	}
	encoded, err := json.Marshal(current.Snapshot)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("encode current arcade dependencies: %w", err)
	}
	result.Status, result.Code, result.DependencyJSON = current.Status, current.Code, string(encoded)
	result.BIOS = current.BIOS
	result.Companions = make([]service.PreparedValidationFile, 0, len(current.Companions))
	for index, resource := range current.Companions {
		result.Companions = append(result.Companions, service.PreparedValidationFile{
			Role: resource.Role, LogicalName: resource.LogicalName, FileRecord: resource.FileRecord, SortOrder: index,
		})
	}
	return result, nil
}

func readReviewArcadeArchives(
	ctx context.Context, executor dbapi.Executor, snapshotID string,
) ([]arcade.Archive, error) {
	rows, err := executor.QueryContext(ctx, `SELECT file.role,file.logical_name,file.file_record,
 entry.normalized_path,entry.uncompressed_size_bytes,entry.crc32,entry.sha1
 FROM import_item_source_snapshot_files file
 LEFT JOIN archive_entries entry ON
 CASE WHEN json_valid(entry.archive_file_record) THEN json_extract(entry.archive_file_record,'$.sha256') END
 =json_extract(file.file_record,'$.sha256')
 WHERE file.source_snapshot_id=? AND file.role IN ('CONTENT','COMPANION')
 ORDER BY file.sort_order,file.logical_name,entry.ordinal`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read imported arcade observations: %w", err)
	}
	defer func() { cleanup.Error("close current review files", rows.Close()) }()
	archives := []arcade.Archive{}
	indexes := map[string]int{}
	for rows.Next() {
		var role, name, record string
		var path, crc, sha *string
		var size *int64
		if err := rows.Scan(&role, &name, &record, &path, &size, &crc, &sha); err != nil {
			return nil, fmt.Errorf("scan imported arcade observations: %w", err)
		}
		index, exists := indexes[name]
		if !exists {
			index = len(archives)
			indexes[name] = index
			archives = append(archives, arcade.Archive{
				Role: role, LogicalName: name, FileRecord: record, Entries: map[string]importing.ArchiveEntry{},
			})
		}
		if path != nil && size != nil && crc != nil && sha != nil {
			archives[index].Entries[*path] = importing.ArchiveEntry{NormalizedPath: *path, Size: *size, CRC32: *crc, SHA1: *sha}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate imported arcade observations: %w", err)
	}
	return archives, nil
}

func observedArcadeMachine(raw string) string {
	var observation struct{ Kind, Machine string }
	if json.Unmarshal([]byte(raw), &observation) != nil || observation.Kind != "ARCADE" {
		return ""
	}
	return observation.Machine
}
