package firmware

import (
	"context"
	"fmt"

	model "retrom/internal/model/firmware"

	"retrom/internal/capability/content/firmware"
)

func expectedArchive(
	ctx context.Context,
	records model.RequirementRecords,
	requirement model.Requirement,
) ([]firmware.ExpectedDATEntry, error) {
	if requirement.ArchiveMembersJSON != nil {
		entries, err := firmware.StaticArchiveExpectations(*requirement.ArchiveMembersJSON)
		if err != nil {
			return nil, fmt.Errorf("decode BIOS archive requirements: %w", err)
		}
		return entries, nil
	}
	entries, err := records.DATEntries(ctx, requirement.ID)
	if err != nil {
		return nil, fmt.Errorf("read BIOS DAT entries: %w", err)
	}
	return entries, nil
}

func (service *Service) InspectArchive(ctx context.Context, id string) (model.ArchiveInspection, error) {
	inspection, err := service.repository.LoadArchiveInspection(ctx, id)
	if err != nil {
		return model.ArchiveInspection{}, fmt.Errorf("inspect BIOS archive: %w", err)
	}
	return inspection, nil
}
