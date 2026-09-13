package firmware

import (
	"context"
	"fmt"

	"retrom/internal/firmware"
	"retrom/internal/importing"
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

func evaluateInstall(ctx context.Context, records RequirementRecords, snapshot installSnapshot,
	actual []importing.ArchiveEntry,
) (string, map[string]any, error) {
	requirement, upload := snapshot.Requirement, snapshot.Upload
	if requirement.FileKind == "ARCHIVE" {
		expected, err := expectedArchive(ctx, records, requirement)
		if err != nil {
			return "", nil, err
		}
		status, details := evaluateArchive(expected, actual, requirement.ArchiveMembersJSON != nil)
		return status, details, nil
	}
	sizeMatched := requirement.Size == nil || *requirement.Size == upload.Size
	md5Matched := requirement.MD5 == nil || *requirement.MD5 == upload.MD5
	sha1Matched := requirement.SHA1 == nil || *requirement.SHA1 == upload.SHA1
	sha256Matched := requirement.SHA256 == nil || *requirement.SHA256 == upload.SHA256
	status := "MATCHED"
	if !sizeMatched || !md5Matched || !sha1Matched || !sha256Matched {
		status = "HASH_WARNING"
	}
	return status, map[string]any{
		"logicalName": requirement.LogicalName, "sourceKind": requirement.SourceKind,
		"sizeMatched": sizeMatched, "md5Matched": md5Matched, "sha1Matched": sha1Matched, "sha256Matched": sha256Matched,
	}, nil
}

func evaluateArchive(expected []firmware.ExpectedDATEntry, actual []importing.ArchiveEntry,
	strict bool,
) (string, map[string]any) {
	comparisons, missing, mismatched, warnings := firmware.CompareArchiveEntries(expected, actual)
	details := map[string]any{
		"schemaVersion":     1,
		"missingEntries":    missing,
		"mismatchedEntries": mismatched,
		"warnings":          warnings,
	}
	if strict && (len(missing) > 0 || len(mismatched) > 0 || len(warnings) > 0) {
		return "INVALID", details
	}
	if len(comparisons) == 0 || len(expected) == 0 || len(missing) > 0 {
		return "MISSING_ENTRY", details
	}
	if len(mismatched) > 0 {
		return "HASH_WARNING", details
	}
	return "MATCHED", details
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
