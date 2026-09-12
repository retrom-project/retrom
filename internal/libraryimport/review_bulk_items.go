package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"retrom/internal/cleanup"

	"github.com/google/uuid"
)

func reviewBulkItemQuery(
	bulkID, outcome, cursor string,
	limit int,
) (string, []any, error) {
	if _, err := uuid.Parse(bulkID); err != nil || limit < 1 || limit > 50 {
		return "", nil, ErrReviewBulkConflict
	}
	if outcome != "" {
		if _, valid := reviewBulkItemOutcomes[outcome]; !valid {
			return "", nil, ErrReviewBulkConflict
		}
	}
	ordinal := -1
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return "", nil, ErrReviewBulkConflict
		}
		ordinal = parsed
	}
	query := `SELECT import_item_id,title_snapshot,target_platform_name_snapshot,state,game_id,
review_event_id,outcome_code,outcome_details_json,completed_at_ms,ordinal
FROM review_bulk_approval_items WHERE bulk_approval_id=? AND ordinal>?`
	arguments := []any{bulkID, ordinal}
	if outcome != "" {
		query += " AND state=?"
		arguments = append(arguments, outcome)
	}
	query += " ORDER BY ordinal LIMIT ?"
	return query, append(arguments, limit+1), nil
}

type projectedReviewBulkItem struct {
	item    ReviewBulkItemResult
	ordinal int
}

func scanReviewBulkItems(rows *sql.Rows, limit int) (ReviewBulkItemPage, error) {
	projectedItems := make([]projectedReviewBulkItem, 0, limit+1)
	for rows.Next() {
		var value projectedReviewBulkItem
		var gameID, eventID, code, details sql.NullString
		var completed sql.NullInt64
		if err := rows.Scan(&value.item.ImportItemID, &value.item.Title, &value.item.PlatformName,
			&value.item.State, &gameID, &eventID, &code, &details, &completed, &value.ordinal); err != nil {
			return ReviewBulkItemPage{}, fmt.Errorf("libraryimport/review bulk items: %w", err)
		}
		value.item.GameID = nullableStringPointer(gameID)
		value.item.ReviewEventID = nullableStringPointer(eventID)
		value.item.OutcomeCode = nullableStringPointer(code)
		value.item.CompletedAtMS = nullableInt64Pointer(completed)
		if details.Valid {
			_ = json.Unmarshal([]byte(details.String), &value.item.OutcomeDetails)
		}
		projectedItems = append(projectedItems, value)
	}
	if err := rows.Err(); err != nil {
		return ReviewBulkItemPage{}, fmt.Errorf("libraryimport/review bulk item rows: %w", err)
	}
	page := ReviewBulkItemPage{Items: make([]ReviewBulkItemResult, 0, min(limit, len(projectedItems)))}
	for index, value := range projectedItems {
		if index == limit {
			next := strconv.Itoa(projectedItems[index-1].ordinal)
			page.NextCursor = &next
			break
		}
		page.Items = append(page.Items, value.item)
	}
	return page, nil
}

func (service *Service) ListReviewBulkItems(
	ctx context.Context,
	bulkID, outcome, cursor string,
	limit int,
) (ReviewBulkItemPage, error) {
	query, arguments, err := reviewBulkItemQuery(bulkID, outcome, cursor, limit)
	if err != nil {
		return ReviewBulkItemPage{}, err
	}
	rows, err := service.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return ReviewBulkItemPage{}, fmt.Errorf("libraryimport/review bulk items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	return scanReviewBulkItems(rows, limit)
}
