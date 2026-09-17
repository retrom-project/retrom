package firmware

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"
	fwmodel "retrom/internal/model/firmware"

	"github.com/google/uuid"
)

func browserInstall(
	ctx context.Context,
	scope writeScope,
	cmd fwmodel.BrowserInstallCommand,
) (fwmodel.Installation, error) {
	current, err := readInstallSnapshot(ctx, scope.readScope, cmd.RequirementID, cmd.FileID, cmd.Version)
	if err != nil {
		return fwmodel.Installation{}, err
	}
	if current.Requirement.SourceKind != cmd.PreparedSourceKind ||
		current.Requirement.FileKind != cmd.PreparedFileKind ||
		current.Upload.BlobID != cmd.PreparedBlobID ||
		current.Upload.SHA256 != cmd.PreparedSHA256 {
		return fwmodel.Installation{}, fwmodel.ErrInvalid
	}
	status, details, err := evaluateInstall(ctx, scope.requirements, current, cmd.ArchiveEntries)
	if err != nil {
		return fwmodel.Installation{}, err
	}
	if status == "INVALID" {
		return fwmodel.Installation{}, &firmware.ArchiveContentError{Details: details}
	}
	now := cmd.NowMS
	if current.Requirement.FileKind == "ARCHIVE" {
		if err := scope.archives.Put(ctx, current.Upload.BlobID, cmd.ArchiveEntries, now); err != nil {
			return fwmodel.Installation{}, fmt.Errorf("record BIOS archive facts: %w", err)
		}
	}
	return persistBrowserInstallation(ctx, scope, current, status, details, now)
}

type installSnapshot struct {
	Requirement fwmodel.Requirement
	Upload      fwmodel.Upload
}

func readInstallSnapshot(
	ctx context.Context,
	scope readScope,
	id, fileID string,
	version int64,
) (installSnapshot, error) {
	requirement, found, err := scope.requirements.Get(ctx, id)
	if err != nil {
		return installSnapshot{}, fmt.Errorf("read BIOS requirement: %w", err)
	}
	if !found || !requirement.Enabled || requirement.Version != version {
		return installSnapshot{}, fwmodel.ErrInvalid
	}
	upload, found, err := scope.uploads.Get(ctx, fileID)
	if err != nil {
		return installSnapshot{}, fmt.Errorf("read BIOS upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" {
		return installSnapshot{}, fwmodel.ErrInvalid
	}
	return installSnapshot{Requirement: requirement, Upload: upload}, nil
}

func evaluateInstall(ctx context.Context, records requirementReader, snapshot installSnapshot,
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

func expectedArchive(
	ctx context.Context,
	records requirementReader,
	requirement fwmodel.Requirement,
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

func evaluateArchive(
	expected []firmware.ExpectedDATEntry,
	actual []importing.ArchiveEntry,
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

func persistBrowserInstallation(ctx context.Context, scope writeScope, snapshot installSnapshot, status string,
	details map[string]any, now int64,
) (fwmodel.Installation, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return fwmodel.Installation{}, fmt.Errorf("generate BIOS installation ID: %w", err)
	}
	consumption, err := uuid.NewV7()
	if err != nil {
		return fwmodel.Installation{}, fmt.Errorf("generate BIOS consumption ID: %w", err)
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return fwmodel.Installation{}, fmt.Errorf("encode BIOS findings: %w", err)
	}
	requirement, upload := snapshot.Requirement, snapshot.Upload
	if err := supersedeInScope(ctx, scope.retirements, requirement.ID, now); err != nil {
		return fwmodel.Installation{}, fmt.Errorf("retire BIOS: %w", err)
	}
	if err := scope.installations.Create(ctx, fwmodel.InstallationWrite{
		ID: id.String(), RequirementID: requirement.ID, BlobID: upload.BlobID, Filename: upload.RelativePath,
		MD5: upload.MD5, SHA1: upload.SHA1, SHA256: upload.SHA256, Size: upload.Size, Status: status,
		RequirementVersion: requirement.Version, DetailsJSON: encoded, AtMS: now, SourceKind: "BROWSER_UPLOAD",
	}); err != nil {
		return fwmodel.Installation{}, fmt.Errorf("persist BIOS installation: %w", err)
	}
	if err := scope.installations.Consume(ctx, fwmodel.Consumption{
		ID: consumption.String(), UploadID: upload.SessionID,
		FileID: upload.ID, InstallationID: id.String(), AtMS: now,
	}); err != nil {
		return fwmodel.Installation{}, fmt.Errorf("consume BIOS upload: %w", err)
	}
	return fwmodel.Installation{
		InstallationID: id.String(), RequirementID: requirement.ID, Status: status, Active: true,
		ValidatedRequirementVersion: requirement.Version, ValidationDetails: details, CreatedAtMS: now,
	}, nil
}
