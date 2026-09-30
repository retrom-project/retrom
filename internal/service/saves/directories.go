package saves

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"

	"github.com/google/uuid"
)

func (service *Service) prepareSaveDirectory(ctx context.Context, launchID string,
	launch Launch, parsed parsedManual,
) (parsedManual, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return parsedManual{}, fmt.Errorf("prepare save directory: %w", err)
	}
	parsed.saveID = id.String()
	if launch.Purpose == "PRODUCT" {
		parsed.saveID, err = service.boundSaveID(ctx, launchID, parsed.saveID)
		if err != nil {
			return parsedManual{}, err
		}
		parsed.directory = "saves/" + parsed.saveID + "/" + id.String()
	} else {
		parsed.directory = "previews/" + launchID + "/checkpoints/" + id.String()
	}
	complete := false
	defer func() {
		if !complete {
			cleanup.Error("discard incomplete save", service.blobs.RemovePath(context.WithoutCancel(ctx), parsed.directory))
		}
	}()
	parsed.payload, err = service.blobs.CopyTo(ctx, parsed.payload.Record, parsed.directory, "payload")
	if err != nil {
		return parsedManual{}, fmt.Errorf("prepare save directory: %w", err)
	}
	if parsed.screenshot != nil {
		image, err := service.blobs.CopyTo(ctx, parsed.screenshot.Record, parsed.directory, "screenshot")
		if err != nil {
			return parsedManual{}, fmt.Errorf("prepare save directory: %w", err)
		}
		parsed.screenshot = &image
	}
	complete = true
	return parsed, nil
}

func (service *Service) boundSaveID(ctx context.Context, launchID, fallback string) (string, error) {
	id := fallback
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		binding, found, err := scope.GameSaves.Binding(ctx, launchID)
		if err != nil {
			return fmt.Errorf("read save binding: %w", err)
		}
		if found && binding.ID != nil {
			id = *binding.ID
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("load save binding: %w", err)
	}
	return id, nil
}
