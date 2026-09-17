package firmware

import (
	"context"
	"fmt"

	model "retrom/internal/model/firmware"
)

func (service *Service) InspectArchive(ctx context.Context, id string) (model.ArchiveInspection, error) {
	inspection, err := service.repository.LoadArchiveInspection(ctx, id)
	if err != nil {
		return model.ArchiveInspection{}, fmt.Errorf("inspect BIOS archive: %w", err)
	}
	return inspection, nil
}
