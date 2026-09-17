package accounts

import (
	"context"
	"fmt"
)

func (service *LinkService) Revoke(
	ctx context.Context,
	actorID, linkID string,
	version int64,
	key string,
) (bool, error) {
	operation, err := newAccountOperation(
		"deleteAdminAccountLink",
		actorID,
		key,
		map[string]any{
			"accountLinkId":   linkID,
			"expectedVersion": version,
		},
		service.now().UnixMilli(),
	)
	if err != nil {
		return false, err
	}
	var replayed bool
	err = service.repository.CommitWrite(ctx, func(scope LinkScope) error {
		replay, err := scope.Read.Replay(ctx, operation)
		if err != nil {
			return fmt.Errorf("apply account link revocation: %w", err)
		}
		if err := checkAccountReplay(replay, operation); err != nil {
			return fmt.Errorf("apply account link revocation: %w", err)
		}
		if replay.Found {
			replayed = true
			return nil
		}
		record, found, err := scope.Read.Current(ctx, linkID)
		if err != nil {
			return fmt.Errorf("apply account link revocation: %w", err)
		}
		if !found {
			return ErrAccountLinkNotActive
		}
		if record.Link.Version != version {
			return ErrUserVersion
		}
		if accountLinkState(record.Link, operation.Now) != "ACTIVE" {
			return ErrAccountLinkNotActive
		}
		if err := scope.Write.Revoke(
			ctx,
			LinkRevocation{
				LinkID:  linkID,
				ActorID: actorID,
				Version: version,
				Now:     operation.Now,
			},
		); err != nil {
			return fmt.Errorf("apply account link revocation: %w", err)
		}
		action := "PASSWORD_RESET_REVOKED"
		if record.Link.Kind == "INVITATION" {
			action = "INVITATION_REVOKED"
		}
		audit, err := newAccountAudit(
			actorID,
			action,
			"ACCOUNT_LINK",
			linkID,
			map[string]any{
				"state": "ACTIVE",
			},
			map[string]any{
				"state": "REVOKED",
			},
			operation.Now,
		)
		if err != nil {
			return fmt.Errorf("apply account link revocation: %w", err)
		}
		if err := scope.Write.Audit(ctx, audit); err != nil {
			return fmt.Errorf("apply account link revocation: %w", err)
		}
		return scope.Write.Remember(ctx, accountReceipt(operation, 204, nil))
	})
	if err != nil {
		return false, fmt.Errorf("revoke account link: %w", err)
	}
	return replayed, nil
}
