package composition

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/authn"
	"retrom/internal/config"
	accountpersistence "retrom/internal/persistence/accounts"
	"retrom/internal/runtime"
	"retrom/internal/service/accounts"
)

func NewAccounts(
	ctx context.Context,
	reader dbapi.DB,
	writer dbapi.DB,
	credentials *runtime.Credentials,
	mode config.Mode,
	blocklist authn.Blocklist,
	now func() time.Time,
) (*accounts.Service, error) {
	hasher := authn.NewPasswordHasher()
	dummy, err := hasher.Hash(ctx, "retrom dummy credential")
	if err != nil {
		return nil, fmt.Errorf("prepare dummy credential: %w", err)
	}
	mint := func() (accounts.SessionMaterial, error) { return accounts.MintSession(rand.Reader) }
	links := accountpersistence.NewLinks(reader, writer)
	modules := accounts.Modules{
		Initialization: accounts.NewInitialization(
			accountpersistence.NewInitialization(
				reader, writer,
			),
			accounts.InitializationOptions{
				Mode:      mode,
				Hasher:    hasher,
				Blocklist: blocklist,
				Mint:      mint,
				Now:       now,
			},
		),
		Authentication: accounts.NewAuthentication(
			accountpersistence.NewAuthentication(reader, writer), hasher, mint, dummy, now),
		Passwords:      accounts.NewPasswords(accountpersistence.NewPasswords(reader, writer), hasher, blocklist, mint, now),
		Directory:      accounts.NewDirectory(accountpersistence.NewDirectory(reader), now),
		Administration: accounts.NewAdministration(accountpersistence.NewAdministration(writer), now),
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
		Limiter: accounts.NewLimiter(accountpersistence.NewRateLimits(writer), credentials, now),
	}
	return accounts.New(modules, mode), nil
}
