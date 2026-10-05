package gamecontent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	contentcapability "retrom/internal/content/capability"
	contentmanifest "retrom/internal/content/manifest"
	contentprepare "retrom/internal/content/prepare"
	contentprofile "retrom/internal/content/profile"
)

func (service *Service) prepareStandardReplacement(ctx context.Context, snapshot JobSnapshot,
	files []UploadedFile,
) (PreparedReplacement, error) {
	if snapshot.ContentMode != contentcapability.ModeStandard || len(files) == 0 ||
		snapshot.PlatformID != "dos" && len(files) != 1 {
		return PreparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_GROUP_INVALID"}
	}
	if snapshot.PlatformID == "arcade" && !strings.EqualFold(filepath.Ext(files[0].LogicalName), ".zip") {
		return PreparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_GROUP_INVALID"}
	}
	if snapshot.PlatformID != "arcade" && snapshot.PlatformID != "dos" {
		prepared, err := contentprepare.New(service.blobs).Single(ctx, snapshot.PlatformID, contentprepare.File{
			LogicalName: files[0].LogicalName, Record: files[0].FileRecord, SHA256: files[0].SHA256, Size: files[0].SizeBytes,
		})
		if err != nil {
			var invalid *contentprepare.Invalid
			if errors.As(err, &invalid) {
				return PreparedReplacement{}, &replacementValidationError{code: invalid.Code}
			}
			return PreparedReplacement{}, fmt.Errorf("prepare replacement content: %w", err)
		}
		files = []UploadedFile{{
			LogicalName: prepared.File.LogicalName, FileRecord: prepared.File.Record,
			SHA256: prepared.File.SHA256, SizeBytes: prepared.File.Size,
		}}
	}
	for _, file := range files {
		if rejection := snapshot.ContentPolicy.CheckFile("SINGLE_FILE", file.LogicalName, file.SizeBytes); rejection != nil {
			return PreparedReplacement{}, &replacementValidationError{code: rejection.Code}
		}
	}
	prepared, err := buildStandardReplacement(files)
	if err != nil {
		return PreparedReplacement{}, err
	}
	if err := service.inspectReplacementRequirements(ctx, snapshot, files[0], &prepared); err != nil {
		return PreparedReplacement{}, err
	}

	return prepared, nil
}

func buildStandardReplacement(files []UploadedFile) (PreparedReplacement, error) {
	replacement := PreparedReplacement{ContentKind: string(contentprofile.ContentKindSingleFile)}
	replacement.Files = make([]ReplacementFile, 0, len(files))
	manifestFiles := make([]contentmanifest.File, 0, len(files))
	for index, file := range files {
		role := "COMPANION"
		if index == 0 {
			role = "CONTENT"
		}
		replacement.Files = append(replacement.Files, ReplacementFile{
			Role: role, LogicalName: file.LogicalName, FileRecord: file.FileRecord,
			SHA256: file.SHA256, SizeBytes: file.SizeBytes, SortOrder: index,
		})
		manifestFiles = append(manifestFiles, contentmanifest.File{
			Role: role, LogicalName: file.LogicalName, BlobSHA256: file.SHA256, SizeBytes: file.SizeBytes,
		})
	}
	replacement.FirstContentLogicalName = files[0].LogicalName
	manifest, digest, err := contentmanifest.Build(replacement.ContentKind, manifestFiles)
	if err != nil {
		return PreparedReplacement{}, &replacementValidationError{code: "GAME_CONTENT_MANIFEST_INVALID"}
	}
	replacement.Manifest, replacement.ManifestDigest = manifest, digest
	return replacement, nil
}
