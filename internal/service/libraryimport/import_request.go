package libraryimport

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/contentprofile"
	model "retrom/internal/model/libraryimport"
)

var ErrMetadataScraperNotConfigured = errors.New("metadata scraper is not configured")

func NormalizeImportRequest(request model.ImportRequest) (model.ImportRequest, string, error) {
	if request.UploadID == "" || request.TargetPlatformInstanceID == "" {
		return model.ImportRequest{}, "", model.ErrInvalid
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
		return model.ImportRequest{}, "", model.ErrInvalid
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
	request model.ImportRequest, mode, purpose, sourceType string, files []model.ImportFile, target model.ImportTarget,
) (model.ImportRequest, string, error) {
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
		return model.ImportRequest{}, "", model.ErrInvalid
	}
	format, reason := ImportArchiveFormat(files[0].Path)
	if reason != "" || (format != contentprofile.ArchiveZIP && format != contentprofile.ArchiveSevenZip) {
		return model.ImportRequest{}, "", model.ErrInvalid
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
		return model.ErrMultiDiscModeUnavailable
	}
	if contentcapability.IsProjectMode(mode) {
		if purpose != "PROJECT" && purpose != "GENERAL" {
			return model.ErrInvalid
		}
		return nil
	}
	if purpose != "GENERAL" {
		return model.ErrInvalid
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
