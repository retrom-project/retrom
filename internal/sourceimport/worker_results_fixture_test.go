package sourceimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/sourceimport"
	application "retrom/internal/service/sourceimport"
)

func (service *Service) finishImport(ctx context.Context, unit work) error {
	completion := application.NewCompletion(repository.NewCompletion(service.database), service.now)
	if err := completion.Finish(ctx, unit.Identity()); err != nil {
		return fmt.Errorf("sourceimport/finish: %w", err)
	}
	return nil
}
