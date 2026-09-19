package libraryimport

import (
	"path/filepath"
	"strings"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/contentprofile"
)

// NormalizeTargetImport applies the virtual platform's transport policy before
// freezing an input. This is a pure function that can be called from any layer.
func NormalizeTargetImport(
	request ImportRequest, mode, purpose, sourceType string, files []ImportFile, target ImportTarget,
) (ImportRequest, string, error) {
	if target.PlatformID != "rpgmaker" {
		return request, mode, nil
	}
	mode = NormalizeTargetImportMode(target.PlatformID, mode)
	if mode == contentcapability.ModeRPGMakerProject {
		request.ContentMode = mode
		request.MetadataProvider = "NONE"
	}
	if mode != contentcapability.ModeRPGMakerProject || purpose != "GENERAL" || sourceType == "DIRECTORY" {
		return request, mode, nil
	}
	if sourceType != "FILES" || len(files) != 1 {
		return ImportRequest{}, "", ErrInvalid
	}
	format, reason := ImportArchiveFormat(files[0].Path)
	if reason != "" || (format != contentprofile.ArchiveZIP && format != contentprofile.ArchiveSevenZip) {
		return ImportRequest{}, "", ErrInvalid
	}
	return request, mode, nil
}

// NormalizeTargetImportMode normalizes the content mode for a given platform.
func NormalizeTargetImportMode(platformID, mode string) string {
	if platformID == "rpgmaker" && mode == contentcapability.ModeStandard {
		return contentcapability.ModeRPGMakerProject
	}
	return mode
}

// ValidateImportUpload validates the upload for the given content mode.
func ValidateImportUpload(mode, sourceType, purpose string) error {
	if mode == contentcapability.ModeMultiDisc && sourceType != "DIRECTORY" {
		return ErrMultiDiscModeUnavailable
	}
	if contentcapability.IsProjectMode(mode) {
		if purpose != "PROJECT" && purpose != "GENERAL" {
			return ErrInvalid
		}
		return nil
	}
	if purpose != "GENERAL" {
		return ErrInvalid
	}
	return nil
}

// ValidImportContentMode returns true if the given mode is a recognized import
// content mode.
func ValidImportContentMode(mode string) bool {
	return mode == contentcapability.ModeStandard || mode == contentcapability.ModeMultiDisc ||
		contentcapability.IsProjectMode(mode)
}

// ImportArchiveFormat returns the archive format for a given file path.
func ImportArchiveFormat(filePath string) (contentprofile.ArchiveFormat, string) {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".zip":
		return contentprofile.ArchiveZIP, ""
	case ".7z":
		return contentprofile.ArchiveSevenZip, ""
	default:
		if strings.HasSuffix(strings.ToLower(filePath), ".7z.001") {
			return "", "ARCHIVE_VOLUME_UNSUPPORTED"
		}
		return "", "UNSUPPORTED_CONTENT_FORMAT"
	}
}
