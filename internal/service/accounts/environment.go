package accounts

import (
	"time"

	"retrom/internal/bootstrap/config"
	"retrom/internal/capability/security/authn"
	accountmodel "retrom/internal/model/accounts"
)

type InitializationOptions struct {
	Mode        config.Mode
	Credentials accountmodel.SetupCredentials
	Hasher      accountmodel.PasswordHasher
	Blocklist   *authn.Blocklist
	Mint        accountmodel.SessionMinter
	Now         func() time.Time
}

type LinkConsumptionOptions struct {
	Tokens    accountmodel.LinkTokenReader
	Hasher    accountmodel.PasswordHasher
	Blocklist *authn.Blocklist
	Mint      accountmodel.SessionMinter
	Now       func() time.Time
}
