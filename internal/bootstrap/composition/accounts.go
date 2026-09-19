package composition

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"

	authnadapter "retrom/internal/adapter/security/authn"

	"retrom/internal/adapter/runtime/runtime"
	"retrom/internal/bootstrap/config"
	"retrom/internal/capability/security/authn"
	accountsmodel "retrom/internal/model/accounts"
	accountpersistence "retrom/internal/repo/accounts"
	"retrom/internal/service/accounts"
)

func NewAccounts(
	ctx context.Context,
	database *sql.DB,
	credentials *runtime.Credentials,
	mode config.Mode,
	blocklist authn.Blocklist,
	now func() time.Time,
) (*accounts.Service, error) {
	hasher := authnadapter.NewPasswordHasher()
	dummy, err := hasher.Hash(ctx, "retrom dummy credential")
	if err != nil {
		return nil, fmt.Errorf("prepare dummy credential: %w", err)
	}
	mint := func() (accountsmodel.SessionMaterial, error) { return accounts.MintSession(rand.Reader) }
	links := accountpersistence.NewLinks(database)
	modules := accounts.Modules{
		Initialization: accounts.NewInitialization(
			accountpersistence.NewInitialization(
				database,
			),
			accounts.InitializationOptions{
				Mode:        mode,
				Credentials: credentials,
				Hasher:      hasher,
				Blocklist:   blocklist,
				Mint:        mint,
				Now:         now,
			},
		),
		Authentication: accounts.NewAuthentication(accountpersistence.NewAuthentication(database), hasher, mint, dummy, now),
		Passwords:      accounts.NewPasswords(accountpersistence.NewPasswords(database), hasher, blocklist, mint, now),
		Recovery:       accounts.NewRecovery(accountpersistence.NewRecovery(database), hasher, blocklist, now),
		Directory:      accounts.NewDirectory(accountpersistence.NewDirectory(database), now),
		Administration: accounts.NewAdministration(accountpersistence.NewAdministration(database), now),
		Links:          accounts.NewLinks(links, credentials, now),
		Issuance:       accounts.NewLinkIssuance(links, credentials, now),
		Consumption: accounts.NewLinkConsumption(
			links,
			accounts.LinkConsumptionOptions{
				Tokens:    credentials,
				Hasher:    hasher,
				Blocklist: blocklist,
				Mint:      mint,
				Now:       now,
			},
		),
		Limiter: accounts.NewLimiter(accountpersistence.NewRateLimits(database), credentials, now),
	}
	return accounts.New(modules, mode), nil
}

func ReadAccountSetupCode(ctx context.Context, database *sql.DB, credentials *runtime.Credentials) (string, error) {
	value, err := accounts.NewInitialization(
		accountpersistence.NewInitialization(
			database,
		),
		accounts.InitializationOptions{
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
