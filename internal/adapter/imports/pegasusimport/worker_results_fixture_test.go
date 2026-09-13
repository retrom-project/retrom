package pegasusimport

import (
	"context"
	"fmt"

	repository "retrom/internal/repo/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) finishImport(ctx context.Context, unit work) error {
	completion := application.NewCompletion(repository.NewCompletion(service.database), service.now)
	if err := completion.Finish(ctx, unit.Identity()); err != nil {
		return fmt.Errorf("pegasusimport/finish: %w", err)
	}
	return nil
}
