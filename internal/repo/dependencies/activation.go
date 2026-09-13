package dependencies

import (
	"context"
	"fmt"

	"retrom/internal/adapter/runtime/dependencies"
	service "retrom/internal/service/dependencies"
)

func (records datRecords) Activation(ctx context.Context, id string) (service.ActivationState, error) {
	var state service.ActivationState
	var active int
	err := records.executor.QueryRowContext(ctx, `
SELECT d.provider_id,d.target_id,d.parse_status,d.is_active
FROM dat_versions d
JOIN runtime_targets target ON target.provider_id=d.provider_id AND target.target_id=d.target_id
WHERE d.id=?
`, id).Scan(&state.Target.ProviderID, &state.Target.TargetID, &state.ParseStatus, &active)
	if err != nil {
		return service.ActivationState{}, fmt.Errorf("dependencies/read DAT activation: %w", err)
	}
	state.Active = active == 1
	return state, nil
}

func (records datRecords) Select(ctx context.Context, input service.DATSelection) error {
	if _, err := records.executor.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0,version=version+1,updated_at_ms=?
WHERE provider_id=? AND target_id=? AND is_active=1 AND id<>?
`, input.AtMS, input.Target.ProviderID, input.Target.TargetID, input.ID); err != nil {
		return fmt.Errorf("dependencies/deselect DAT: %w", err)
	}
	result, err := records.executor.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=1,activated_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND provider_id=? AND target_id=?
  AND parse_status='READY' AND is_active=0
`, input.AtMS, input.AtMS, input.ID, input.Target.ProviderID, input.Target.TargetID)
	if err != nil {
		return fmt.Errorf("dependencies/select DAT: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("dependencies/count DAT selection: %w", err)
	}
	if changed != 1 {
		return dependencies.ErrInvalid
	}
	if _, err := records.executor.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,created_at_ms)
VALUES(?,?,?,?,'BUILTIN_DAT_ACTIVATED','DAT_VERSION',?,
'{"active":false}','{"active":true}',json_object('source','release-manifest'),?)
`, input.AuditID, input.Actor.Kind, input.Actor.UserID, input.Actor.Label, input.ID, input.AtMS); err != nil {
		return fmt.Errorf("dependencies/audit DAT selection: %w", err)
	}
	return nil
}
