package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/pegasusimport"
)

type PlanLifecycle struct{ database *sql.DB }

func NewPlanLifecycle(database *sql.DB) *PlanLifecycle { return &PlanLifecycle{database: database} }
func (repository *PlanLifecycle) WithPlanWrite(ctx context.Context, work func(application.PlanRecords) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus plan write: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(planRecords{executor: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus plan write: %w", err)
	}
	return nil
}

func (repository *PlanLifecycle) ExpiredPlans(
	ctx context.Context,
	now int64,
	limit int,
) ([]application.ExpiredPlan, error) {
	rows, err := repository.database.QueryContext(
		ctx,
		`SELECT id,version FROM pegasus_imports
WHERE state='AWAITING_MAPPING' AND expires_at_ms<=? ORDER BY expires_at_ms,id LIMIT ?`,
		now,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query expired Pegasus plans: %w", err)
	}
	defer func() { cleanup.Error("close expired Pegasus plans", rows.Close()) }()
	result := []application.ExpiredPlan{}
	for rows.Next() {
		var value application.ExpiredPlan
		if err := rows.Scan(
			&value.ID,
			&value.Version,
		); err != nil {
			return nil, fmt.Errorf(
				"scan expired Pegasus plan: %w",
				err,
			)
		}
		result = append(
			result,
			value,
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired Pegasus plans: %w", err)
	}
	return result, nil
}

type planRecords struct{ executor dbexec.Executor }

func (records planRecords) Get(
	ctx context.Context,
	id string,
) (application.Summary, error) {
	return (&Queries{database: records.executor}).Get(ctx, id)
}

func (records planRecords) Delete(ctx context.Context, plan application.PlanDeletion) error {
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE pegasus_imports SET version=version WHERE id=? AND version=?
AND state IN ('AWAITING_MAPPING','EXPIRED') AND import_job_id IS NULL`,
		plan.Before.ID,
		plan.Before.Version,
	)
	if err := requirePlanChange(result, err); err != nil {
		return err
	}
	if _, err := records.executor.ExecContext(ctx, `
UPDATE tags SET version=version+1,updated_by_user_id=?,updated_at_ms=?
WHERE id IN (SELECT relation.tag_id FROM pegasus_collection_tags relation
JOIN pegasus_import_collections collection ON collection.id=relation.collection_id WHERE collection.import_id=?)
`, plan.ActorID, plan.NowMS, plan.Before.ID); err != nil {
		return fmt.Errorf("touch deleted Pegasus tag relations: %w", err)
	}
	for _, statement := range []string{
		`DELETE FROM pegasus_collection_tags
WHERE collection_id IN (SELECT id FROM pegasus_import_collections WHERE import_id=?)`,

		`DELETE FROM pegasus_import_item_assets WHERE item_id IN (SELECT id FROM pegasus_import_items WHERE import_id=?)`,

		`DELETE FROM pegasus_import_item_files WHERE item_id IN (SELECT id FROM pegasus_import_items WHERE import_id=?)`,

		`DELETE FROM pegasus_import_items WHERE import_id=?`,

		`DELETE FROM pegasus_import_collections WHERE import_id=?`,

		`DELETE FROM pegasus_import_metadata_files WHERE import_id=?`,

		`DELETE FROM pegasus_imports WHERE id=?`,
	} {
		if _, err := records.executor.ExecContext(
			ctx,
			statement,
			plan.Before.ID,
		); err != nil {
			return fmt.Errorf(
				"delete mutable Pegasus projection: %w",
				err,
			)
		}
	}
	return records.deletionAudit(ctx, plan)
}

func (records planRecords) deletionAudit(ctx context.Context, plan application.PlanDeletion) error {
	before, err := json.Marshal(
		map[string]any{"state": plan.Before.State, "version": plan.Before.Version},
	)
	if err != nil {
		return fmt.Errorf(
			"encode Pegasus deletion audit: %w",
			err,
		)
	}
	_, err = records.executor.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'USER',?,NULL,'PEGASUS_IMPORT_DELETED','PEGASUS_IMPORT',?,?,NULL,NULL,NULL,?)
`, plan.AuditID, plan.ActorID, plan.Before.ID, string(before), plan.NowMS)
	if err != nil {
		return fmt.Errorf("record Pegasus deletion audit: %w", err)
	}
	return nil
}

func (records planRecords) Expire(ctx context.Context, plan application.PlanExpiry) error {
	result, err := recordstore.UpdatePegasusImports(ctx, records.executor, recordstore.Update{
		Set: `state='EXPIRED',phase=NULL,last_error_code='PEGASUS_PLAN_EXPIRED',
completed_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state='AWAITING_MAPPING' AND expires_at_ms<=?`,
			Args:  []any{plan.Before.ID, plan.Before.Version, plan.NowMS},
		}, Values: []any{
			plan.NowMS,
			plan.NowMS,
		},
	})
	return requirePlanChange(result, err)
}

func requirePlanChange(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("change Pegasus plan: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read Pegasus plan change count: %w", err)
	}
	if count != 1 {
		return application.ErrInvalid
	}
	return nil
}
