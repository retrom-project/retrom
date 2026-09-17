package importdiscard

import "context"

func (service *Service) recoverSourceLinks(ctx context.Context, key Key) error {
	err := service.repository.CommitRecoverOwnership(ctx, RecoverOwnershipCommand{Key: key})
	return failure("recover source ownership", err)
}
