package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ReviewQueue struct{ database dbapi.DB }

func NewReviewQueue(database dbapi.DB) *ReviewQueue { return &ReviewQueue{database: database} }

func (repository *ReviewQueue) List(
	ctx context.Context, query libraryservice.ReviewQueueQuery,
) ([]libraryservice.ReviewQueueRecord, error) {
	transaction, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin review queue read: %w", err)
	}
	defer dbapi.Rollback(transaction)
	current := make([]libraryservice.ReviewQueueRecord, 0, query.Limit)
	for len(current) < query.Limit {
		page, err := readReviewQueuePage(ctx, transaction, query)
		if err != nil {
			return nil, err
		}
		for _, record := range page {
			runtime, err := ReadReviewRuntime(ctx, transaction, record.ItemID)
			if err != nil {
				return nil, err
			}
			record.ValidationStatus, record.CompatibilityCode = &runtime.Status, &runtime.Code
			if query.Filter.BlockerCode != "" && runtime.Code != query.Filter.BlockerCode {
				continue
			}
			current = append(current, record)
			if len(current) == query.Limit {
				break
			}
		}
		if len(page) < query.Limit || query.Filter.BlockerCode == "" {
			break
		}
		last := page[len(page)-1]
		query.After = &libraryservice.ReviewQueuePosition{UpdatedAtMS: last.UpdatedAtMS, ItemID: last.ItemID}
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit review queue read: %w", err)
	}
	return current, nil
}

func readReviewQueuePage(
	ctx context.Context, executor dbapi.Executor, query libraryservice.ReviewQueueQuery,
) ([]libraryservice.ReviewQueueRecord, error) {
	statement, args := reviewQueueStatement(query)
	rows, err := executor.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query review queue: %w", err)
	}
	defer func() { cleanup.Error("close review queue", rows.Close()) }()
	result := make([]libraryservice.ReviewQueueRecord, 0, query.Limit)
	for rows.Next() {
		record, err := scanReviewQueueRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review queue: %w", err)
	}
	return result, nil
}

func reviewQueueStatement(query libraryservice.ReviewQueueQuery) (string, []any) {
	statement := reviewQueueSelect
	args := make([]any, 0, 12)
	for _, filter := range []struct{ value, condition string }{
		{query.Filter.ImportJobID, " AND i.import_job_id=?"},
		{query.Filter.SourceImportID, " AND source.import_id=?"},
		{query.Filter.PlatformInstanceID, " AND d.target_platform_instance_id=?"},
	} {
		if filter.value != "" {
			statement += filter.condition
			args = append(args, filter.value)
		}
	}
	if query.Filter.Query != "" {
		statement += ` AND (strpos(i.search_text,?)>0 OR EXISTS(
 SELECT 1 FROM review_draft_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=d.id AND strpos(tag.name_key,?)>0))`
		args = append(args, query.Filter.Query, query.Filter.Query)
	}
	if query.Filter.TagID != "" {
		statement += ` AND EXISTS(SELECT 1 FROM review_draft_tags relation
 JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=d.id AND tag.id=?)`
		args = append(args, query.Filter.TagID)
	}
	comparison, direction := ">", "ASC"
	if query.Filter.Sort == "UPDATED_DESC" {
		comparison, direction = "<", "DESC"
	}
	if query.After != nil {
		statement += " AND (d.review_updated_at_ms" + comparison +
			"? OR (d.review_updated_at_ms=? AND i.id" + comparison + "?))"
		args = append(args, query.After.UpdatedAtMS, query.After.UpdatedAtMS, query.After.ItemID)
	}
	statement += " ORDER BY d.review_updated_at_ms " + direction + ",i.id " + direction
	statement += " LIMIT ?"
	args = append(args, query.Limit)
	return statement, args
}

func scanReviewQueueRecord(scanner dbapi.Scanner) (libraryservice.ReviewQueueRecord, error) {
	var result libraryservice.ReviewQueueRecord
	var sourceID, sourceImportID, sourceLabel *string
	var sourceCover bool
	err := scanner.Scan(
		&result.ItemID, &result.Version, &result.ImportJobID, &result.DraftTitle, &result.SourceName,
		&result.Platform.ID, &result.Platform.Name, &result.ValidationStatus, &result.CompatibilityCode,
		&result.UpdatedAtMS, &result.CandidateCount, &result.SourceTotalSizeBytes, &result.SourceMD5, &result.CoverAssetID,
		&sourceID, &sourceImportID, &sourceLabel, &sourceCover,
	)
	if err != nil {
		return libraryservice.ReviewQueueRecord{}, fmt.Errorf("scan review queue item: %w", err)
	}
	if sourceID != nil && sourceImportID != nil {
		result.Source = &libraryservice.ReviewQueueSource{
			ItemID: *sourceID, ImportID: *sourceImportID, Label: sourceLabel, HasCover: sourceCover,
		}
	}
	return result, nil
}
