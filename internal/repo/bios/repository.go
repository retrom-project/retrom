package bios

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/bios"
	"retrom/internal/repo/dbexec"
)

type Repository struct {
	database *sql.DB
}

func New(database *sql.DB) *Repository {
	return &Repository{database: database}
}

const statusExpression = "COALESCE(installation.status," +
	"CASE WHEN requirement.requirement_mode='OPTIONAL' THEN 'OPTIONAL_MISSING' ELSE 'MISSING' END)"

func (repository *Repository) List(
	ctx context.Context,
	request application.ListRequest,
) (application.ListResult, error) {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ListResult{}, fmt.Errorf("begin BIOS catalog snapshot: %w", err)
	}
	defer dbexec.Rollback(transaction)

	result, err := repository.list(ctx, transaction, request)
	if err != nil {
		return application.ListResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return application.ListResult{}, fmt.Errorf("commit BIOS catalog snapshot: %w", err)
	}
	return result, nil
}

func (repository *Repository) list(
	ctx context.Context,
	executor dbexec.Executor,
	request application.ListRequest,
) (application.ListResult, error) {
	counts, err := scopeCounts(ctx, executor)
	if err != nil {
		return application.ListResult{}, err
	}
	summary, err := summary(ctx, executor, request.Scope)
	if err != nil {
		return application.ListResult{}, err
	}
	filteredCount, err := filteredCount(ctx, executor, request)
	if err != nil {
		return application.ListResult{}, err
	}
	items, err := listItems(ctx, executor, request)
	if err != nil {
		return application.ListResult{}, err
	}
	return application.ListResult{
		ScopeCounts:   counts,
		Summary:       summary,
		FilteredCount: filteredCount,
		Items:         items,
	}, nil
}

func scopeCounts(ctx context.Context, executor dbexec.Executor) (application.ScopeCounts, error) {
	var result application.ScopeCounts
	err := executor.QueryRowContext(ctx, `
SELECT COALESCE(sum(CASE WHEN `+scopeSQL(application.ScopeRequiredByLibrary)+` THEN 1 ELSE 0 END),0),count(*)
FROM bios_requirements requirement WHERE requirement.enabled=1
`).Scan(&result.RequiredByLibrary, &result.FullCatalog)
	if err != nil {
		return application.ScopeCounts{}, fmt.Errorf("aggregate BIOS scopes: %w", err)
	}
	return result, nil
}

func summary(
	ctx context.Context,
	executor dbexec.Executor,
	scope string,
) (application.Summary, error) {
	var result application.Summary
	err := executor.QueryRowContext(ctx, `
SELECT count(*),
COALESCE(sum(CASE WHEN requirement.requirement_mode<>'OPTIONAL' AND `+statusExpression+`
 IN ('MISSING','INVALID') THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN `+statusExpression+` IN ('HASH_WARNING','MISSING_ENTRY') THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN `+statusExpression+`='MATCHED' THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN (requirement.requirement_mode<>'OPTIONAL' AND `+statusExpression+`
 IN ('MISSING','INVALID')) OR `+statusExpression+` IN ('HASH_WARNING','MISSING_ENTRY') THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN requirement.requirement_mode='REQUIRED' THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN requirement.requirement_mode='OPTIONAL' THEN 1 ELSE 0 END),0)
FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
 AND installation.is_active=1 WHERE requirement.enabled=1 AND `+scopeSQL(scope)).Scan(
		&result.TotalCount,
		&result.BlockingCount,
		&result.WarningCount,
		&result.ReadyCount,
		&result.AttentionCount,
		&result.RequiredCount,
		&result.OptionalCount,
	)
	if err != nil {
		return application.Summary{}, fmt.Errorf("aggregate BIOS status: %w", err)
	}
	return result, nil
}

func filteredCount(
	ctx context.Context,
	executor dbexec.Executor,
	request application.ListRequest,
) (int64, error) {
	conditions, arguments := conditions(request, false)
	var count int64
	err := executor.QueryRowContext(ctx, `
SELECT count(*) FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
AND installation.is_active=1 WHERE `+joinConditions(conditions), arguments...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("aggregate filtered BIOS catalog: %w", err)
	}
	return count, nil
}

func listItems(
	ctx context.Context,
	executor dbexec.Executor,
	request application.ListRequest,
) ([]application.Item, error) {
	conditions, arguments := conditions(request, true)
	query := `
SELECT requirement.id,requirement.core_id,core.name,requirement.provider_id,requirement.target_id,
requirement.logical_name,
requirement.source_kind,requirement.file_kind,requirement.requirement_mode,
requirement.condition_code,requirement.md5,requirement.enabled,
requirement.version,` + statusExpression + `,installation.id,installation.md5,installation.sha1,installation.sha256,
installation.validated_requirement_version,installation.created_at_ms
FROM bios_requirements requirement JOIN cores core ON core.id=requirement.core_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id AND installation.is_active=1
WHERE ` + joinConditions(conditions) + ` ORDER BY core.name COLLATE BINARY,
requirement.logical_name COLLATE BINARY,requirement.id COLLATE BINARY LIMIT ?`
	arguments = append(arguments, request.Limit)
	rows, err := executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query BIOS catalog: %w", err)
	}
	defer func() { cleanup.Error("close BIOS catalog", rows.Close()) }()

	items := make([]application.Item, 0, request.Limit)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate BIOS catalog: %w", err)
	}
	return items, nil
}

func scanItem(scanner dbexec.Scanner) (application.Item, error) {
	var item application.Item
	var conditionCode, expectedMD5 sql.NullString
	var installationID, installedMD5, installedSHA1, installedSHA256 sql.NullString
	var validatedVersion, installedAt sql.NullInt64
	var enabled int64
	if err := scanner.Scan(
		&item.ID,
		&item.CoreID,
		&item.CoreName,
		&item.ProviderID,
		&item.TargetID,
		&item.LogicalName,
		&item.SourceKind,
		&item.FileKind,
		&item.RequirementMode,
		&conditionCode,
		&expectedMD5,
		&enabled,
		&item.Version,
		&item.Status,
		&installationID,
		&installedMD5,
		&installedSHA1,
		&installedSHA256,
		&validatedVersion,
		&installedAt,
	); err != nil {
		return application.Item{}, fmt.Errorf("scan BIOS catalog item: %w", err)
	}
	item.ConditionCode = nullableString(conditionCode)
	item.ExpectedMD5 = nullableString(expectedMD5)
	item.Enabled = enabled == 1
	if installationID.Valid {
		item.ActiveInstallation = &application.Installation{
			ID: installationID.String, MD5: installedMD5.String, SHA1: installedSHA1.String,
			SHA256: installedSHA256.String, ValidatedRequirementVersion: validatedVersion.Int64,
			CreatedAtMS: installedAt.Int64,
		}
	}
	return item, nil
}

func conditions(request application.ListRequest, includeCursor bool) ([]string, []any) {
	result := []string{
		"requirement.enabled=1",
		scopeSQL(request.Scope),
	}
	arguments := make([]any, 0, 12)
	if request.Query != "" {
		result = append(
			result,
			"(instr(lower(requirement.logical_name),lower(?))>0 OR instr(lower(core.name),lower(?))>0)",
		)
		arguments = append(arguments, request.Query, request.Query)
	}
	for _, filter := range []struct {
		value, column string
	}{
		{request.CoreID, "requirement.core_id"},
		{request.ProviderID, "requirement.provider_id"},
		{request.TargetID, "requirement.target_id"},
	} {
		if filter.value != "" {
			result = append(result, filter.column+"=?")
			arguments = append(arguments, filter.value)
		}
	}
	if request.PlatformID != "" {
		result = append(result, `EXISTS(SELECT 1 FROM platform_cores platform_core
WHERE platform_core.core_id=requirement.core_id AND platform_core.platform_id=?)`)
		arguments = append(arguments, request.PlatformID)
	}
	if request.Status != "" {
		result = append(result, statusExpression+"=?")
		arguments = append(arguments, request.Status)
	}
	result = appendQuickFilter(result, request.Quick)
	if includeCursor && request.Cursor != nil {
		result = append(
			result,
			"(core.name>? OR (core.name=? AND requirement.logical_name>?) OR "+
				"(core.name=? AND requirement.logical_name=? AND requirement.id>?))",
		)
		arguments = append(
			arguments,
			request.Cursor.SortValues[0], request.Cursor.SortValues[0], request.Cursor.SortValues[1],
			request.Cursor.SortValues[0], request.Cursor.SortValues[1], request.Cursor.ID,
		)
	}
	return result, arguments
}

func appendQuickFilter(conditions []string, quick string) []string {
	switch quick {
	case application.QuickAttention:
		return append(conditions, "((requirement.requirement_mode<>'OPTIONAL' AND "+
			statusExpression+" IN ('MISSING','INVALID')) OR "+
			statusExpression+" IN ('HASH_WARNING','MISSING_ENTRY'))")
	case application.QuickRequired:
		return append(conditions, "requirement.requirement_mode='REQUIRED'")
	case application.QuickOptional:
		return append(conditions, "requirement.requirement_mode='OPTIONAL'")
	default:
		return conditions
	}
}

func scopeSQL(scope string) string {
	if scope == application.ScopeFullCatalog {
		return "1=1"
	}
	return `EXISTS(SELECT 1 FROM game_variants variant
JOIN games game ON game.id=variant.game_id
WHERE variant.provider_id=requirement.provider_id AND variant.target_id=requirement.target_id
 AND game.status='PUBLISHED')`
}

func joinConditions(conditions []string) string {
	result := ""
	for index, condition := range conditions {
		if index > 0 {
			result += " AND "
		}
		result += condition
	}
	return result
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
