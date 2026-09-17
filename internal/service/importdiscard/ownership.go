package importdiscard

import (
	model "retrom/internal/model/importdiscard"
	"context"
)

func (service *Service) recoverSourceLinks(ctx context.Context, key model.Key) error {
	err := service.repository.CommitRecoverOwnership(ctx, model.RecoverOwnershipCommand{Key: key})
	return failure("recover source ownership", err)
}
