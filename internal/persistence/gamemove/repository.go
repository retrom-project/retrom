package gamemove

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/gamemove"
)

type Repository struct{ database *sql.DB }

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func (repository *Repository) ImpactSubject(
	ctx context.Context, gameID, targetID string,
) (application.ImpactSubject, error) {
	var subject application.ImpactSubject
	var datID sql.NullString
	err := repository.database.QueryRowContext(ctx, `
SELECT g.id,
g.platform_instance_id,
src.platform_id,
COALESCE(content.logical_name,''),
g.version,
target.id,
target.platform_id,
target.default_core_id,
target.version,
binding.provider_id,
binding.target_id,
(SELECT id
FROM dat_versions
WHERE provider_id=binding.provider_id AND target_id=binding.target_id
AND is_active=1)
FROM games g
JOIN platform_instances src ON src.id=g.platform_instance_id
LEFT JOIN game_files content ON content.game_id=g.id
AND content.role='CONTENT'
JOIN platform_instances target ON target.id=?
AND target.enabled=1
AND target.deleted_at_ms IS NULL
JOIN runtime_target_bindings binding ON binding.core_id=target.default_core_id
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=target.platform_id AND binding_platform.core_id=target.default_core_id
JOIN runtime_targets runtime_target ON runtime_target.provider_id=binding.provider_id
 AND runtime_target.target_id=binding.target_id
WHERE g.id=?
AND g.status='PUBLISHED'
`, targetID, gameID).Scan(
		&subject.GameID,
		&subject.SourcePlatformInstanceID,
		&subject.SourcePlatformID,
		&subject.ContentLogicalName,
		&subject.GameVersion,
		&subject.TargetPlatformInstanceID,
		&subject.TargetPlatformID,
		&subject.TargetCoreID,
		&subject.TargetPlatformVersion,
		&subject.TargetProviderID,
		&subject.TargetID,
		&datID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ImpactSubject{}, application.ErrImpactStale
	}
	if err != nil {
		return application.ImpactSubject{}, fmt.Errorf("read move impact subject: %w", err)
	}
	if datID.Valid {
		subject.TargetDATVersionID = &datID.String
	}
	return subject, nil
}

func (repository *Repository) Variant(
	ctx context.Context, query application.VariantQuery,
) (application.VariantState, bool, error) {
	var state application.VariantState
	err := repository.database.QueryRowContext(ctx, `
SELECT status,
compatibility_code
FROM game_variants
WHERE game_id=?
AND core_id=?
AND provider_id=?
AND target_id=?
AND dat_version_id IS ?
	`, query.GameID, query.CoreID, query.ProviderID, query.TargetID, nullableString(query.DATVersionID)).
		Scan(&state.Status, &state.CompatibilityCode)
	if errors.Is(err, sql.ErrNoRows) {
		return application.VariantState{}, false, nil
	}
	if err != nil {
		return application.VariantState{}, false, fmt.Errorf("read move variant: %w", err)
	}
	return state, true, nil
}

func (repository *Repository) QueuedJobState(ctx context.Context, jobID string) (string, error) {
	var state string
	if err := repository.database.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state); err != nil {
		return "", fmt.Errorf("read move validation job: %w", err)
	}
	return state, nil
}

func (repository *Repository) LatestScrapeRun(
	ctx context.Context, gameID string,
) (string, bool, error) {
	var runID string
	err := repository.database.QueryRowContext(ctx, `
SELECT r.id
FROM metadata_scrape_runs r
JOIN games g ON g.id=r.game_id AND g.status='PUBLISHED'
WHERE r.game_id=?
AND r.provider='HASHEOUS'
AND r.state='COMPLETED'
ORDER BY r.created_at_ms DESC,
r.id DESC LIMIT 1
`, gameID).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read latest game scrape run: %w", err)
	}
	return runID, true, nil
}

func (repository *Repository) ScrapeCandidates(
	ctx context.Context, runID string,
) ([]application.CandidateRecord, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT id,
provider_game_id,
normalized_metadata_json,
evidence_json,
created_at_ms,
(SELECT count(*)
FROM scrape_candidate_hits h
WHERE h.scrape_candidate_id=c.id)
FROM scrape_candidates c
WHERE scrape_run_id=?
ORDER BY created_at_ms,
id
`, runID)
	if err != nil {
		return nil, fmt.Errorf("query game scrape candidates: %w", err)
	}
	defer func() { cleanup.Error("close game scrape candidates", rows.Close()) }()
	result := make([]application.CandidateRecord, 0)
	for rows.Next() {
		var record application.CandidateRecord
		if err := rows.Scan(
			&record.ID,
			&record.ProviderGameID,
			&record.MetadataJSON,
			&record.EvidenceJSON,
			&record.CreatedAtMS,
			&record.HitCount,
		); err != nil {
			return nil, fmt.Errorf("scan game scrape candidate: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game scrape candidates: %w", err)
	}
	return result, nil
}

func (repository *Repository) WithMove(
	ctx context.Context, work func(application.MoveScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin game move: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(moveScope{transaction: transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit game move: %w", err)
	}
	return nil
}

type moveScope struct{ transaction *sql.Tx }

func (scope moveScope) UpdateGame(
	ctx context.Context, gameID, targetID string, expectedVersion, nowMS int64,
) (bool, error) {
	result, err := recordstore.UpdateGames(ctx, scope.transaction, recordstore.Update{
		Set: `
platform_instance_id=?,
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
id=?
AND version=?
`,
			Args: []any{gameID, expectedVersion},
		},
		Values: []any{targetID, nowMS},
	})
	if err != nil {
		return false, fmt.Errorf("update moved game: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count moved game update: %w", err)
	}
	return changed == 1, nil
}

func (scope moveScope) Audit(ctx context.Context, event application.AuditEvent) error {
	var beforeJSON, afterJSON any
	if event.Before != nil {
		value, err := json.Marshal(event.Before)
		if err != nil {
			return fmt.Errorf("encode game move audit before state: %w", err)
		}
		beforeJSON = string(value)
	}
	if event.After != nil {
		value, err := json.Marshal(event.After)
		if err != nil {
			return fmt.Errorf("encode game move audit after state: %w", err)
		}
		afterJSON = string(value)
	}
	_, err := scope.transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,
actor_kind,
actor_user_id,
actor_label,
action,
resource_type,
resource_id,
before_json,
after_json,
diff_json,
request_id,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
'{}',
?,
?)
`, event.ID,
		event.Actor.Kind,
		event.Actor.UserID,
		event.Actor.Label,
		event.Action,
		event.ResourceType,
		event.ResourceID,
		beforeJSON,
		afterJSON,
		event.Actor.RequestID,
		event.CreatedAtMS,
	)
	if err != nil {
		return fmt.Errorf("insert game move audit: %w", err)
	}
	return nil
}

var _ application.Repository = (*Repository)(nil)
