package serverimport

import (
	"context"

	"retrom/internal/repo/recordstore"
	"retrom/internal/service/serverimport"
)

func (records outcomeRecords) Item(ctx context.Context, plan serverimport.ItemOutcome) error {
	var details *string
	if plan.Details != nil {
		value := string(plan.Details)
		details = &value
	}
	result, err := recordstore.UpdateServerBiosImportItems(ctx, records.executor, recordstore.Update{
		Set: `state=?,match_method=?,selection_details_json=?,outcome_code=?,previous_installation_id=NULL,
 new_installation_id=NULL,completed_at_ms=?,updated_at_ms=?`, Scope: recordstore.Scope{
			Where: `server_import_id=? AND requirement_id=? AND state IN ('PENDING','EVALUATING')`, Args: []any{
				plan.Unit.ImportID,
				plan.RequirementID,
			},
		},
		Values: []any{plan.State, plan.Method, details, plan.Code, plan.Now, plan.Now},
	})
	if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
		return err
	}
	if plan.CandidateID != nil {
		result, err := records.executor.ExecContext(ctx, `UPDATE server_bios_import_candidates SET state=?,
 not_selected_reason=?,updated_at_ms=? WHERE id=? AND server_import_id=? AND requirement_id=?`,
			plan.CandidateState, plan.Code, plan.Now, plan.CandidateID, plan.Unit.ImportID, plan.RequirementID)
		if err := requireControlChange(result, err, serverimport.ErrLeaseLost); err != nil {
			return err
		}
	}
	return records.event(ctx, plan.Unit, "PROGRESS", plan.Event, plan.Now)
}
