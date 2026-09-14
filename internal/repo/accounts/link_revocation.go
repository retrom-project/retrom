package accounts

import (
	"context"

	"retrom/internal/model/accounts"
	"retrom/internal/repo/recordstore"
)

func (records linkRecords) Revoke(ctx context.Context, plan accounts.LinkRevocation) error {
	result, err := recordstore.UpdateAccountLinks(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `revoked_at_ms=?,revoked_by_kind='USER',revoked_by_user_id=?,version=version+1`,
			Scope: recordstore.Scope{
				Where: `id=? AND version=? AND consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?`,
				Args: []any{
					plan.LinkID,
					plan.Version,
					plan.Now,
				},
			},
			Values: []any{
				plan.Now,
				plan.ActorID,
			},
		},
	)
	return authChanged(result, err, accounts.ErrAccountLinkNotActive)
}
