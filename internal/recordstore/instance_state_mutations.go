package recordstore

import (
	"context"
	"database/sql"
)

func UpdateInstanceState(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"instance_state",
		"id,state,test_default_password_active",
		InstanceStateUpdateRule,
	)
}

const InstanceStateUpdateRule = `
WITH previous(id,state,test_default_password_active) AS (VALUES(?,?,?))
SELECT CASE
-- instance_state_default_password_no_reenable
WHEN ((candidate.test_default_password_active IS NOT previous.test_default_password_active) AND
(previous.state='COMPLETED' AND previous.test_default_password_active=0 AND
candidate.test_default_password_active=1)) THEN 'test default password cannot be re-enabled'
-- instance_state_no_reopen
WHEN ((candidate.state IS NOT previous.state) AND (previous.state='COMPLETED' AND
candidate.state!='COMPLETED')) THEN 'initialization is terminal'
ELSE '' END
FROM instance_state candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
