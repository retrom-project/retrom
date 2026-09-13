package serverimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	"retrom/internal/service/serverimport"
)

func (repository *Queries) Get(ctx context.Context, importID string) (serverimport.Summary, error) {
	return getSummary(ctx, repository.database, importID)
}

func getSummary(ctx context.Context, executor dbexec.Executor, importID string) (serverimport.Summary, error) {
	result, err := scanSummary(executor.QueryRowContext(ctx, summaryQuery+` WHERE import.id=?`, importID))
	if errors.Is(err, sql.ErrNoRows) {
		return serverimport.Summary{}, serverimport.ErrNotFound
	}
	return result, err
}

const summaryQuery = `
SELECT import.id,import.kind,import.root_id,import.root_label_snapshot,import.source_relative_path,
import.replace_if_better,import.state,import.phase,import.catalog_item_count,import.candidate_count,
import.evaluated_item_count,import.imported_matched_count,import.imported_warning_count,
import.imported_missing_entry_count,import.not_found_count,import.skipped_existing_count,
import.skipped_not_better_count,import.same_bytes_count,import.failed_item_count,import.cancelled_item_count,
import.job_id,import.created_by_user_id,user.display_name,import.last_error_code,import.version,
import.created_at_ms,import.updated_at_ms,import.completed_at_ms
FROM server_imports import JOIN users user ON user.id=import.created_by_user_id`

type Queries struct{ database *sql.DB }

func NewQueries(database *sql.DB) *Queries { return &Queries{database} }

// The fixed aggregate projection is scanned in schema order for auditability.
func scanSummary(row dbexec.Scanner) (serverimport.Summary, error) {
	var summary serverimport.Summary
	var replace int
	var catalog, candidates, evaluated, matched, warnings, missingEntry int64
	var notFound, skippedExisting, skippedNotBetter, same, failed, cancelled int64
	if err := row.Scan(
		&summary.ID, &summary.Kind, &summary.Root.ID, &summary.Root.Label, &summary.SourceRelativePath,
		&replace, &summary.State, &summary.Phase, &catalog, &candidates, &evaluated, &matched, &warnings, &missingEntry,
		&notFound, &skippedExisting, &skippedNotBetter, &same, &failed, &cancelled, &summary.JobID,
		&summary.CreatedBy.ID, &summary.CreatedBy.DisplayName, &summary.LastErrorCode, &summary.Version,
		&summary.CreatedAtMS, &summary.UpdatedAtMS, &summary.CompletedAtMS,
	); err != nil {
		return serverimport.Summary{}, fmt.Errorf("serverimport/scan summary: %w", err)
	}
	summary.ReplaceIfBetter = replace == 1
	summary.Counts = serverimport.Counts{
		CatalogItems: catalog, Candidates: candidates, EvaluatedItems: evaluated,
		Imported: matched + warnings + missingEntry, Matched: matched, Warnings: warnings + missingEntry,
		NotFound: notFound, Skipped: skippedExisting + skippedNotBetter + same,
		Conflicts: skippedNotBetter, Failed: failed, Cancelled: cancelled,
	}
	return summary, nil
}

// The keyset predicate and stable ordering stay adjacent to the query call.
func (repository *Queries) List(ctx context.Context, query serverimport.ListQuery) ([]serverimport.Summary, error) {
	state, beforeAt, beforeID, limit := query.State, int64(0), "", query.Limit
	if query.Before != nil {
		beforeAt, beforeID = query.Before.CreatedAtMS, query.Before.ID
	}

	conditions := []string{"1=1"}
	arguments := []any{}
	if state != "" {
		conditions = append(conditions, "import.state=?")
		arguments = append(arguments, state)
	}
	if beforeID != "" {
		conditions = append(conditions, "(import.created_at_ms<? OR (import.created_at_ms=? AND import.id<?))")
		arguments = append(arguments, beforeAt, beforeAt, beforeID)
	}
	arguments = append(arguments, limit)
	rows, err := repository.database.QueryContext(
		ctx,
		summaryQuery+" WHERE "+strings.Join(
			conditions,
			" AND ",
		)+" ORDER BY import.created_at_ms DESC,import.id DESC LIMIT ?",
		arguments...)
	if err != nil {
		return nil, fmt.Errorf("serverimport/list summaries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]serverimport.Summary, 0)
	for rows.Next() {
		summary, scanErr := scanSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("serverimport/iterate summaries: %w", err)
	}
	return result, nil
}

func (repository *Queries) Items(ctx context.Context, filter serverimport.ItemQuery) ([]serverimport.Item, error) {
	importID, query, outcome, method, limit := filter.ImportID, filter.Text, filter.Outcome, filter.Method, filter.Limit
	afterCore, afterName, afterID := "", "", ""
	if filter.After != nil {
		afterCore, afterName, afterID = filter.After.Core, filter.After.Name, filter.After.ID
	}

	conditions := []string{"item.server_import_id=?"}
	arguments := []any{importID}
	if query != "" {
		conditions = append(
			conditions,
			"(instr(lower(item.logical_name),lower(?))>0 OR "+
				"instr(lower(item.core_name_snapshot),lower(?))>0 OR instr(lower(item.core_id),lower(?))>0)",
		)
		arguments = append(arguments, query, query, query)
	}
	if outcome != "" {
		conditions = append(conditions, "item.state=?")
		arguments = append(arguments, outcome)
	}
	if method != "" {
		conditions = append(conditions, "item.match_method=?")
		arguments = append(arguments, method)
	}
	if afterID != "" {
		conditions = append(
			conditions,
			"(item.core_name_snapshot>? OR (item.core_name_snapshot=? AND item.logical_name>?) OR "+
				"(item.core_name_snapshot=? AND item.logical_name=? AND item.requirement_id>?))",
		)
		arguments = append(arguments, afterCore, afterCore, afterName, afterCore, afterName, afterID)
	}
	arguments = append(arguments, limit)
	rows, err := repository.database.QueryContext(ctx, `
SELECT item.requirement_id,item.core_id,item.core_name_snapshot,item.provider_id,item.target_id,
item.logical_name,
item.requirement_mode,
item.source_kind,item.state,item.candidate_count,item.match_method,item.outcome_code,item.selection_details_json,
selected.relative_path,previous.status,replacement.status,
CASE WHEN item.previous_installation_id IS NOT NULL AND item.new_installation_id IS NOT NULL
          AND item.previous_installation_id<>item.new_installation_id THEN 1 ELSE 0 END
FROM server_bios_import_items item
LEFT JOIN server_bios_import_candidates selected ON selected.server_import_id=item.server_import_id
 AND selected.requirement_id=item.requirement_id AND selected.state='SELECTED'
LEFT JOIN bios_installations previous ON previous.id=item.previous_installation_id
LEFT JOIN bios_installations replacement ON replacement.id=item.new_installation_id
WHERE `+strings.Join(conditions, " AND ")+`
 ORDER BY item.core_name_snapshot COLLATE BINARY,item.logical_name COLLATE BINARY,
 item.requirement_id COLLATE BINARY LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("serverimport/list items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]serverimport.Item, 0)
	for rows.Next() {
		var item serverimport.Item
		var details sql.NullString
		var replaced int
		if err := rows.Scan(
			&item.RequirementID, &item.CoreID, &item.CoreName, &item.ProviderID, &item.TargetID,
			&item.LogicalName,
			&item.RequirementMode, &item.SourceKind, &item.State, &item.CandidateCount, &item.MatchMethod,
			&item.OutcomeCode, &details, &item.SelectedRelativePath, &item.PreviousInstallationStatus,
			&item.NewInstallationStatus, &replaced,
		); err != nil {
			return nil, fmt.Errorf("serverimport/scan item: %w", err)
		}
		item.Replaced = replaced == 1
		if details.Valid {
			if err := json.Unmarshal([]byte(details.String), &item.SelectionDetails); err != nil {
				return nil, fmt.Errorf("decode server import selection evidence: %w", err)
			}
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("serverimport/iterate items: %w", err)
	}
	return result, nil
}

// serverimport.Candidate evidence is decoded in the same order as its stable keyset query.
func (repository *Queries) Candidates(
	ctx context.Context,
	filter serverimport.CandidateQuery,
) ([]serverimport.Candidate, error) {
	importID, requirementID, limit := filter.ImportID, filter.RequirementID, filter.Limit
	afterRank, afterID := int64(0), ""
	if filter.After != nil {
		afterRank, afterID = filter.After.Rank, filter.After.ID
	}

	var exists int
	if err := repository.database.QueryRowContext(ctx, `
SELECT 1 FROM server_bios_import_items WHERE server_import_id=? AND requirement_id=?
`, importID, requirementID).Scan(&exists); errors.Is(
		err,
		sql.ErrNoRows,
	) {
		return nil, serverimport.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("serverimport/check candidate item: %w", err)
	}
	arguments := []any{importID, requirementID}
	watermark := ""
	if afterID != "" {
		watermark = " AND (COALESCE(rank_ordinal,9223372036854775807)>? OR " +
			"(COALESCE(rank_ordinal,9223372036854775807)=? AND id>?))"
		arguments = append(arguments, afterRank, afterRank, afterID)
	}
	arguments = append(arguments, limit)
	rows, err := repository.database.QueryContext(ctx, `
SELECT id,relative_path,basename,association_kind,size_bytes,md5,sha1,sha256,crc32,state,
rank_ordinal,not_selected_reason,evaluation_details_json
FROM server_bios_import_candidates WHERE server_import_id=? AND requirement_id=?`+watermark+`
 ORDER BY COALESCE(rank_ordinal,9223372036854775807),id LIMIT ?`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("serverimport/list candidates: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]serverimport.Candidate, 0)
	for rows.Next() {
		var candidate serverimport.Candidate
		var details sql.NullString
		if err := rows.Scan(
			&candidate.ID, &candidate.RelativePath, &candidate.Basename, &candidate.AssociationKind,
			&candidate.SizeBytes, &candidate.MD5, &candidate.SHA1, &candidate.SHA256, &candidate.CRC32,
			&candidate.State, &candidate.RankOrdinal, &candidate.NotSelectedReason, &details,
		); err != nil {
			return nil, fmt.Errorf("serverimport/scan candidate: %w", err)
		}
		if details.Valid {
			if err := json.Unmarshal([]byte(details.String), &candidate.EvaluationDetails); err != nil {
				return nil, fmt.Errorf("decode server import candidate evidence: %w", err)
			}
		}
		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("serverimport/iterate candidates: %w", err)
	}
	return result, nil
}
