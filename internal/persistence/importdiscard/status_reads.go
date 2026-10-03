package importdiscard

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/service/importdiscard"
)

// StatusFacts uses two bounded queries for the entire page, regardless of its size.
func (records records) StatusFacts(ctx context.Context, kind string, ids []string) (
	map[string]importdiscard.StatusFacts, error,
) {
	table, err := batchTable(kind)
	if err != nil {
		return nil, err
	}
	result := make(map[string]importdiscard.StatusFacts, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, kind)
	for _, id := range ids {
		args = append(args, id)
	}
	if err := records.readStatusHeads(ctx, table, placeholders, args, result); err != nil {
		return nil, err
	}
	if err := records.readStatusCounts(ctx, kind, table, placeholders, args[1:], result); err != nil {
		return nil, err
	}
	return result, nil
}

func (records records) readStatusHeads(ctx context.Context, table, placeholders string, args []any,
	result map[string]importdiscard.StatusFacts,
) error {
	fields := "batch.import_job_id IS NOT NULL,0,0,''"
	if table == "import_jobs" {
		fields = "1,batch.rejected_file_count,batch.resolved_rejected_file_count,batch.payload_state"
	}
	rows, err := records.executor.QueryContext(ctx,
		`SELECT batch.id,batch.state,batch.version,`+fields+`,disposition.state,disposition.error_code
FROM `+table+` batch LEFT JOIN import_batch_discards disposition
ON disposition.kind=? AND disposition.import_id=batch.id
WHERE batch.id IN (`+placeholders+`)`, args...)
	if err != nil {
		return fmt.Errorf("read discard page heads: %w", err)
	}
	defer func() { cleanup.Error("close discard page heads", rows.Close()) }()
	for rows.Next() {
		var id string
		var fact importdiscard.StatusFacts
		var state, code sql.NullString
		batch := &fact.Batch
		if err := rows.Scan(&id, &batch.State, &batch.Version, &batch.Started, &batch.Rejected,
			&batch.ResolvedRejected, &batch.PayloadState, &state, &code); err != nil {
			return fmt.Errorf("scan discard page head: %w", err)
		}
		batch.ItemCounts = make(map[string]int64)
		if state.Valid {
			fact.Disposition = &importdiscard.Disposition{State: state.String}
			if code.Valid {
				fact.Disposition.ErrorCode = &code.String
			}
		}
		result[id] = fact
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate discard page heads: %w", err)
	}
	return nil
}

func (records records) readStatusCounts(ctx context.Context, kind, table, placeholders string, args []any,
	result map[string]importdiscard.StatusFacts,
) error {
	items, state, foreignKey := table[:len(table)-1]+"_items", "execution_state", "import_id"
	if kind == "IMPORT" {
		items, state, foreignKey = "import_items", "state", "import_job_id"
	}
	rows, err := records.executor.QueryContext(ctx, `SELECT `+foreignKey+`,`+state+`,count(*) FROM `+items+
		` WHERE `+foreignKey+` IN (`+placeholders+`) GROUP BY `+foreignKey+`,`+state, args...)
	if err != nil {
		return fmt.Errorf("read discard page counts: %w", err)
	}
	defer func() { cleanup.Error("close discard page counts", rows.Close()) }()
	for rows.Next() {
		var id, itemState string
		var count int64
		if err := rows.Scan(&id, &itemState, &count); err != nil {
			return fmt.Errorf("scan discard page count: %w", err)
		}
		if fact, found := result[id]; found {
			fact.Batch.ItemCounts[itemState] = count
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate discard page counts: %w", err)
	}
	return nil
}
