package composition

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"

	"retrom/internal/adapter/runtime/runtime"
	"retrom/internal/capability/security/authn"
	accountsmodel "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
	accountsservice "retrom/internal/service/accounts"
)

func NewAccounts(
	ctx context.Context,
	database *sql.DB,
	credentials *runtime.Credentials,
	mode accountsmodel.Mode,
	blocklist authn.Blocklist,
	now func() time.Time,
) (*accountsservice.Service, error) {
	hasher := authn.NewPasswordHasher()
	dummy, err := hasher.Hash(ctx, "retrom dummy credential")
	if err != nil {
		return nil, fmt.Errorf("prepare dummy credential: %w", err)
	}
	mint := func() (accountsmodel.SessionMaterial, error) { return accountsservice.MintSession(rand.Reader) }
	links := accountpersistence.NewLinks(database)
	modules := accountsservice.Modules{
		Initialization: accountsservice.NewInitialization(
			accountpersistence.NewInitialization(
				database,
			),
		accountsservice.InitializationOptions{
			Mode:        mode,
			Credentials: credentials,
			Hasher:      hasher,
			Blocklist:   blocklist,
			Mint:        mint,
				Now:         now,
			},
		),
		Authentication: accountsservice.NewAuthentication(
			accountpersistence.NewAuthentication(database),
			hasher,
			mint,
			dummy,
			now,
		),
		Passwords: accountsservice.NewPasswords(
			accountpersistence.NewPasswords(database),
			hasher,
			blocklist,
			mint,
			now,
		),
		Recovery:       accountsservice.NewRecovery(accountpersistence.NewRecovery(database), hasher, blocklist, now),
		Directory:      accountsservice.NewDirectory(accountpersistence.NewDirectory(database), now),
		Administration: accountsservice.NewAdministration(accountpersistence.NewAdministration(database), now),
		Links:          accountsservice.NewLinks(links, credentials, now),
		Issuance:       accountsservice.NewLinkIssuance(links, credentials, now),
		Consumption: accountsservice.NewLinkConsumption(
			links,
			accountsservice.LinkConsumptionOptions{
				Tokens:    credentials,
				Hasher:    hasher,
				Blocklist: blocklist,
				Mint:      mint,
				Now:       now,
			},
		),
		Limiter: accountsservice.NewLimiter(accountpersistence.NewRateLimits(database), credentials, now),
	}
	return accountsservice.New(modules, mode), nil
}

func ReadAccountSetupCode(ctx context.Context, database *sql.DB, credentials *runtime.Credentials) (string, error) {
	value, err := accountsservice.NewInitialization(
		accountpersistence.NewInitialization(
			database,
		),
		accountsservice.InitializationOptions{
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
