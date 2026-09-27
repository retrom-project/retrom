package launch

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

func productCreationOwner(
	ctx context.Context,
	executor dbapi.Executor,
	command application.ProductCreateCommand,
) (bool, error) {
	if command.ActorID == "" {
		return true, nil
	}
	var valid bool
	err := dbapi.QueryRowContext(ctx, executor, `
SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND profile_id=? AND status='ENABLED')`,
		command.ActorID, command.ProfileID,
	).Scan(
		&valid,
	)
	if err != nil {
		return false, fmt.Errorf("read product owner: %w", err)
	}
	return valid, nil
}
