package importdiscard

import (
	"context"

	model "retrom/internal/model/importdiscard"
)

func (service *Service) recoverSourceLinks(ctx context.Context, key model.Key) error {
	err := service.repository.CommitRecoverOwnership(ctx, model.RecoverOwnershipCommand{Key: key})
	return failure("recover source ownership", err)
}
