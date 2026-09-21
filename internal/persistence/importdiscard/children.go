package importdiscard

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/importdiscard"
)

func (records records) Children(ctx context.Context, key importdiscard.Key) ([]string, error) {
	kind, id := key.Kind, key.ID
	if kind == "IMPORT" {
		return []string{id}, nil
	}
	table, err := batchTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := records.executor.QueryContext(ctx, `
SELECT DISTINCT library_import_job_id FROM `+table[:len(table)-1]+`_items
WHERE import_id=? AND library_import_job_id IS NOT NULL
UNION SELECT job.id FROM server_import_upload_owners owner
JOIN import_jobs job ON job.upload_session_id=owner.upload_session_id
JOIN `+table[:len(table)-1]+`_items item ON item.id=owner.source_item_id
WHERE owner.kind=? AND item.import_id=? ORDER BY 1`, id, kind, id)
	if err != nil {
		return nil, fmt.Errorf("importdiscard/list child imports: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var ids []string
	for rows.Next() {
		var item string
		if err := rows.Scan(&item); err != nil {
			return nil, fmt.Errorf("importdiscard/read child import: %w", err)
		}
		ids = append(ids, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("importdiscard/read child imports: %w", err)
	}
	return ids, nil
}
