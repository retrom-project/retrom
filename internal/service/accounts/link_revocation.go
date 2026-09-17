package accounts

import (
	"context"
	"fmt"

	model "retrom/internal/model/accounts"

	"github.com/google/uuid"
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
	auditID, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("create link revocation audit identity: %w", err)
	}
	err = service.repository.CommitRevokeLink(ctx, model.RevokeLinkCommand{
		LinkID:    linkID,
		ActorID:   actorID,
		Version:   version,
		AuditID:   auditID.String(),
		Operation: operation,
	})
	if err != nil {
		return false, fmt.Errorf("revoke account link: %w", err)
	}
	return false, nil
}
