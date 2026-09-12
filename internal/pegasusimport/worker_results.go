package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) closeItem(ctx context.Context, unit work, itemID, state, code string, retryable bool) {
	service.closeItemWithFailure(ctx, unit, itemID, state, code, retryable, nil)
}

func (service *Service) closeItemWithFailure(
	ctx context.Context,
	unit work,
	itemID, state, code string,
	retryable bool,
	failure *FailureDetails,
) {
	outcome := application.ItemOutcome{State: state, Code: code, Retryable: retryable, Failure: failure}
	service.finishItem(ctx, unit, itemID, outcome)
}

func (service *Service) finishItem(ctx context.Context, unit work, itemID string, outcome application.ItemOutcome) {
	items := application.NewItemWork(repository.NewItemWork(service.database), service.now)
	if err := items.Finish(ctx, unit.Identity(), itemID, outcome); err != nil {
		slog.Error("Pegasus item completion failed", "error", service.sanitizeTechnicalDetail(err))
	}
}

func mediaWarning(kind string, err error) string {
	if errors.Is(err, ErrSourceChanged) {
		return "PEGASUS_SOURCE_CHANGED"
	}
	if kind == "COVER" {
		return "PEGASUS_IMAGE_INVALID"
	}
	return "PEGASUS_VIDEO_UNSUPPORTED"
}

func (service *Service) finishImport(ctx context.Context, unit work) error {
	completion := application.NewCompletion(repository.NewCompletion(service.database), service.now)
	if err := completion.Finish(ctx, unit.Identity()); err != nil {
		return fmt.Errorf("pegasusimport/finish: %w", err)
	}
	return nil
}
