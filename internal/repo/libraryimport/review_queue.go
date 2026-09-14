package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

type ReviewQueue struct{ database *sql.DB }

func NewReviewQueue(database *sql.DB) *ReviewQueue { return &ReviewQueue{database: database} }

func (repository *ReviewQueue) List(
	ctx context.Context, query application.ReviewQueueQuery,
) ([]application.ReviewQueueRecord, error) {
	statement, args := reviewQueueStatement(query)
	rows, err := repository.database.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query review queue: %w", err)
	}
	defer func() { cleanup.Error("close review queue", rows.Close()) }()
	result := make([]application.ReviewQueueRecord, 0, query.Limit)
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
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close review queue: %w", err)
	}
	return result, nil
}

func reviewQueueStatement(query application.ReviewQueueQuery) (string, []any) {
	statement := reviewQueueSelect
	args := make([]any, 0, 12)
	for _, filter := range []struct{ value, condition string }{
		{query.Filter.ImportJobID, " AND i.import_job_id=?"},
		{query.Filter.PegasusImportID, " AND pegasus.import_id=?"},
		{query.Filter.EmulationStationImportID, " AND emulationstation.import_id=?"},
		{query.Filter.PlatformInstanceID, " AND d.target_platform_instance_id=?"},
	} {
		if filter.value != "" {
			statement += filter.condition
			args = append(args, filter.value)
		}
	}
	if query.Filter.Query != "" {
		statement += ` AND (instr(i.search_text,?)>0 OR EXISTS(
 SELECT 1 FROM review_draft_tags relation JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=d.id AND instr(tag.name_key,?)>0))`
		args = append(args, query.Filter.Query, query.Filter.Query)
	}
	if query.Filter.TagID != "" {
		statement += ` AND EXISTS(SELECT 1 FROM review_draft_tags relation
 JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
 WHERE relation.review_draft_id=d.id AND tag.id=?)`
		args = append(args, query.Filter.TagID)
	}
	if query.Filter.BlockerCode != "" {
		statement += " AND (v.compatibility_code=? OR (?='NEEDS_VALIDATION' AND v.id IS NULL))"
		args = append(args, query.Filter.BlockerCode, query.Filter.BlockerCode)
	}
	comparison, direction := ">", "ASC"
	if query.Filter.Sort == "UPDATED_DESC" {
		comparison, direction = "<", "DESC"
	}
	if query.After != nil {
		statement += " AND (d.updated_at_ms" + comparison + "? OR (d.updated_at_ms=? AND i.id" + comparison + "?))"
		args = append(args, query.After.UpdatedAtMS, query.After.UpdatedAtMS, query.After.ItemID)
	}
	statement += " ORDER BY d.updated_at_ms " + direction + ",i.id " + direction + " LIMIT ?"
	args = append(args, query.Limit)
	return statement, args
}

func scanReviewQueueRecord(scanner dbexec.Scanner) (application.ReviewQueueRecord, error) {
	var result application.ReviewQueueRecord
	var pegasusID, pegasusImportID, pegasusLabel, esID, esImportID, esLabel *string
	var pegasusCover, esCover bool
	err := scanner.Scan(
		&result.ItemID, &result.Version, &result.ImportJobID, &result.DraftTitle, &result.SourceName,
		&result.Platform.ID, &result.Platform.Name, &result.ValidationStatus, &result.CompatibilityCode,
		&result.UpdatedAtMS, &result.CandidateCount, &result.SourceTotalSizeBytes, &result.SourceMD5, &result.CoverAssetID,
		&pegasusID, &pegasusImportID, &pegasusLabel, &pegasusCover, &esID, &esImportID, &esLabel, &esCover,
	)
	if err != nil {
		return application.ReviewQueueRecord{}, fmt.Errorf("scan review queue item: %w", err)
	}
	if pegasusID != nil && pegasusImportID != nil {
		result.Pegasus = &application.ReviewQueueSource{
			ItemID: *pegasusID, ImportID: *pegasusImportID, Label: pegasusLabel, HasCover: pegasusCover,
		}
	}
	if esID != nil && esImportID != nil {
		result.EmulationStation = &application.ReviewQueueSource{
			ItemID: *esID, ImportID: *esImportID, Label: esLabel, HasCover: esCover,
		}
	}
	return result, nil
}
