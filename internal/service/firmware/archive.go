package firmware

import (
	"context"
	"fmt"

	"retrom/internal/capability/content/firmware"
)

func expectedArchive(
	ctx context.Context,
	records RequirementRecords,
	requirement Requirement,
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

func (service *Service) InspectArchive(ctx context.Context, id string) (ArchiveInspection, error) {
	var inspection ArchiveInspection
	err := service.repository.WithRead(ctx, func(scope ReadScope) error {
		requirement, found, err := scope.Requirements.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("read BIOS inspection requirement: %w", err)
		}
		if !found || !requirement.Enabled || requirement.FileKind != "ARCHIVE" {
			return ErrArchiveFactsNotFound
		}
		active, found, err := scope.Installations.Active(ctx, id)
		if err != nil {
			return fmt.Errorf("read active BIOS inspection: %w", err)
		}
		if !found {
			return ErrArchiveFactsNotFound
		}
		expected, err := expectedArchive(ctx, scope.Requirements, requirement)
		if err != nil {
			return err
		}
		actual, err := scope.Archives.Entries(ctx, active.BlobID)
		if err != nil {
			return fmt.Errorf("read BIOS archive inspection: %w", err)
		}
		comparisons, _, _, _ := firmware.CompareArchiveEntries(expected, actual)
		inspection = ArchiveInspection{
			RequirementID: id, LogicalName: requirement.LogicalName,
			InstallationID: active.ID, InstallationStatus: active.Status, Entries: comparisons,
		}
		return nil
	})
	if err != nil {
		return ArchiveInspection{}, fmt.Errorf("inspect BIOS archive: %w", err)
	}
	return inspection, nil
}
