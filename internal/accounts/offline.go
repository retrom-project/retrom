package accounts

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	accountpersistence "retrom/internal/persistence/accounts"
	accountservice "retrom/internal/service/accounts"

	retromruntime "retrom/internal/runtime"
)

var ErrOfflineAdmin = accountservice.ErrOfflineAdmin

func ReadSetupCode(
	ctx context.Context,
	database *sql.DB,
	credentials *retromruntime.Credentials,
) (string, error) {
	value, err := accountservice.NewInitialization(
		accountpersistence.NewInitialization(
			database,
		),
		accountservice.InitializationOptions{
			Credentials: credentials,
		},
	).ReadSetupCode(
		ctx,
	)
	if err != nil {
		return "", fmt.Errorf("read setup-code state: %w", err)
	}
	return value, nil
}

func (service *Service) OfflineAdminReset(ctx context.Context, username, password, confirmation string) error {
	recovery := accountservice.NewRecovery(
		accountpersistence.NewRecovery(
			service.database,
		),
		service.hasher,
		service.blocklist,
		func() time.Time {
			return service.now()
		},
	)
	if err := recovery.Reset(ctx, username, password, confirmation); err != nil {
		return fmt.Errorf("recover offline administrator: %w", err)
	}
	return nil
}
