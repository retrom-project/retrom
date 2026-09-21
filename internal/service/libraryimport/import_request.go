package libraryimport

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"retrom/internal/contentcapability"
	"retrom/internal/contentprofile"
)

var ErrMetadataScraperNotConfigured = errors.New("metadata scraper is not configured")

func NormalizeImportRequest(request ImportRequest) (ImportRequest, string, error) {
	if request.UploadID == "" || request.TargetPlatformInstanceID == "" {
		return ImportRequest{}, "", ErrInvalid
	}
	request.TagIDs = slices.Clone(request.TagIDs)
	if request.TagIDs == nil {
		request.TagIDs = []string{}
	}
	mode := request.ContentMode
	if mode == "" {
		mode = contentcapability.ModeStandard
	}
	if !ValidImportContentMode(mode) || (request.MetadataProvider != "NONE" && request.MetadataProvider != "HASHEOUS") {
		return ImportRequest{}, "", ErrInvalid
	}
	if contentcapability.IsProjectMode(mode) {
		request.MetadataProvider = "NONE"
	}
	return request, mode, nil
}

func ValidImportContentMode(mode string) bool {
	return mode == contentcapability.ModeStandard || mode == contentcapability.ModeMultiDisc ||
		contentcapability.IsProjectMode(mode)
}

// NormalizeTargetImport applies the virtual platform's transport policy before freezing an input.
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

func NormalizeTargetImportMode(platformID, mode string) string {
	if platformID == "rpgmaker" && mode == contentcapability.ModeStandard {
		return contentcapability.ModeRPGMakerProject
	}
	return mode
}

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
