package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	contentcapability "retrom/internal/content/capability"
	"retrom/internal/content/diagnostic"
	"retrom/internal/content/requirements"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/contentquery"
)

func applyReviewInputRejection(
	ctx context.Context, executor dbapi.Executor, current ReviewRuntime,
) (ReviewRuntime, error) {
	var snapshot struct {
		ContentFacts *requirements.Facts `json:"contentFacts"`
	}
	if err := json.Unmarshal([]byte(current.DependencyJSON), &snapshot); err != nil {
		return ReviewRuntime{}, fmt.Errorf("read current review content facts: %w", err)
	}
	rejection, rejected, err := reviewInputRejection(ctx, executor, current, snapshot.ContentFacts)
	if err != nil {
		return ReviewRuntime{}, err
	}
	var failure *diagnostic.Rejection
	if rejected {
		failure = &rejection
		current.Status, current.Code = "BLOCKED", rejection.Code
	}
	current.DependencyJSON, err = diagnostic.WithRejection(current.DependencyJSON, failure)
	if err != nil {
		return ReviewRuntime{}, fmt.Errorf("update review content diagnostics: %w", err)
	}
	return current, nil
}

func reviewInputRejection(ctx context.Context, executor dbapi.Executor, current ReviewRuntime,
	parsed *requirements.Facts,
) (diagnostic.Rejection, bool, error) {
	var policy contentcapability.Policy
	if err := dbapi.QueryRowContext(ctx, executor, `SELECT `+contentquery.BindingPolicySQL+`
 FROM runtime_target_bindings binding WHERE binding.provider_id=? AND binding.target_id=?`,
		current.ProviderID, current.TargetID,
	).Scan(contentquery.ScanPolicy(&policy)); err != nil {
		return diagnostic.Rejection{}, false, fmt.Errorf("read review input policy: %w", err)
	}
	if err := contentquery.LoadRequirements(ctx, executor, policy.Requirements); err != nil {
		return diagnostic.Rejection{}, false, fmt.Errorf("resolve review requirements: %w", err)
	}
	if policy.Requirements != nil {
		facts := requirements.Facts{}
		if parsed != nil {
			facts = *parsed
		}
		name, err := BindReviewInputs(executor).ContentLogicalName(ctx, current.SnapshotID)
		if err != nil {
			return diagnostic.Rejection{}, false, fmt.Errorf("resolve review requirements: %w", err)
		}
		if rejection := policy.Requirements.Evaluate(facts, name); rejection != nil {
			return *rejection, true, nil
		}
	}
	if policy.MaxFileBytes(current.ContentKind) == 0 {
		return diagnostic.Rejection{}, false, nil
	}
	var name string
	var size int64
	err := dbapi.QueryRowContext(ctx, executor, `SELECT logical_name,((file_record)::jsonb ->> 'size_bytes')::bigint
 FROM import_item_source_snapshot_files
 WHERE source_snapshot_id=? AND role IN ('CONTENT','DISC','PROJECT_FILE','DOS_SOURCE')
 ORDER BY ((file_record)::jsonb ->> 'size_bytes')::bigint DESC,logical_name LIMIT 1`,
		current.SnapshotID).Scan(&name, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return diagnostic.Rejection{}, false, nil
	}
	if err != nil {
		return diagnostic.Rejection{}, false, fmt.Errorf("read review delivered content size: %w", err)
	}
	if rejection := policy.CheckFile(current.ContentKind, name, size); rejection != nil {
		return *rejection, true, nil
	}
	return diagnostic.Rejection{}, false, nil
}
