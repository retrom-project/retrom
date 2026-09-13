package maintenance

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/service/maintenance"
)

func (writes writes) Audit(ctx context.Context, audit maintenance.FenceAudit) error {
	contents, err := json.Marshal(audit.Counts)
	if err != nil {
		return fmt.Errorf("encode restore audit: %w", err)
	}
	_, err = writes.transaction.ExecContext(ctx, `INSERT INTO audit_events(
 id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms)
 VALUES(?,'SYSTEM',NULL,'restore-security-fence','RESTORE_SECURITY_FENCE','INSTANCE','instance',
 NULL,?,'{}',NULL,?)`, audit.ID, string(contents), audit.Now)
	if err != nil {
		return fmt.Errorf("persist restore audit: %w", err)
	}
	return nil
}
