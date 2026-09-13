package serverimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	"retrom/internal/service/serverimport"
)

type Discovery struct{ database *sql.DB }

func NewDiscovery(database *sql.DB) *Discovery { return &Discovery{database} }
func (repository *Discovery) WithWrite(ctx context.Context, work func(serverimport.DiscoveryRecords) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin discovery write: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(discoveryRecords{tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit discovery write: %w", err)
	}
	return nil
}

type discoveryRecords struct{ executor dbexec.Executor }

func (records discoveryRecords) Reset(ctx context.Context, unit serverimport.Work, now int64) error {
	if err := LockWorker(ctx, records.executor, unit, now, RunningWorker); err != nil {
		return err
	}
	if _, err := records.executor.ExecContext(
		ctx,
		`DELETE FROM server_bios_import_candidates WHERE server_import_id=?`,
		unit.ImportID,
	); err != nil {
		return fmt.Errorf("delete discovery evidence: %w", err)
	}
	if _, err := recordstore.UpdateServerBiosImportItems(ctx, records.executor, recordstore.Update{
		Set: `state='PENDING',candidate_count=0,match_method=NULL,selection_details_json=NULL,
 previous_installation_id=NULL,new_installation_id=NULL,outcome_code=NULL,completed_at_ms=NULL,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `server_import_id=? AND state IN ('PENDING','EVALUATING')`,
			Args: []any{
				unit.ImportID,
			},
		}, Values: []any{
			now,
		},
	}); err != nil {
		return fmt.Errorf("reset discovery items: %w", err)
	}
	return nil
}

func (records discoveryRecords) Persist(ctx context.Context, plan serverimport.DiscoveryPlan) error {
	if err := LockWorker(ctx, records.executor, plan.Unit, plan.Now, RunningWorker); err != nil {
		return err
	}
	for _, group := range plan.Groups {
		for _, candidate := range group.Candidates {
			if err := records.candidate(ctx, plan.Unit, candidate, plan.Now); err != nil {
				return err
			}
		}
		result, err := recordstore.UpdateServerBiosImportItems(ctx, records.executor, recordstore.Update{
			Set: `state='EVALUATING',candidate_count=?,updated_at_ms=?`, Scope: recordstore.Scope{
				Where: `server_import_id=? AND requirement_id=? AND state IN ('PENDING','EVALUATING')`, Args: []any{
					plan.Unit.ImportID,
					group.RequirementID,
				},
			}, Values: []any{len(group.Candidates), plan.Now},
		})
		if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
			return err
		}
	}
	result, err := records.executor.ExecContext(
		ctx,
		`UPDATE server_imports SET phase='RANKING',candidate_count=?,
 evaluated_item_count=catalog_item_count,multi_candidate_item_count=?,skipped_special_count=?,
 skipped_unrepresentable_path_count=?,version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'`,

		plan.Total,
		plan.Multiple,
		plan.Counts.SkippedSpecial,
		plan.Counts.SkippedUnrepresentable,
		plan.Now,
		plan.Unit.ImportID,
	)
	return requireControlChange(result, err, serverimport.ErrLeaseLost)
}
