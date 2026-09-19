package libraryimport

import (
	"errors"
	"slices"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/contentprofile"
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
	if !model.ValidImportContentMode(mode) || (request.MetadataProvider != "NONE" && request.MetadataProvider != "HASHEOUS") {
		return model.ImportRequest{}, "", model.ErrInvalid
	}
	if contentcapability.IsProjectMode(mode) {
		request.MetadataProvider = "NONE"
	}
	return request, mode, nil
}

// ValidImportContentMode delegates to the model-level pure function.
func ValidImportContentMode(mode string) bool {
	return model.ValidImportContentMode(mode)
}

// NormalizeTargetImport delegates to the model-level pure function.
func NormalizeTargetImport(
	request model.ImportRequest, mode, purpose, sourceType string, files []model.ImportFile, target model.ImportTarget,
) (model.ImportRequest, string, error) {
	return model.NormalizeTargetImport(request, mode, purpose, sourceType, files, target)
}

// NormalizeTargetImportMode delegates to the model-level pure function.
func NormalizeTargetImportMode(platformID, mode string) string {
	return model.NormalizeTargetImportMode(platformID, mode)
}

// ValidateImportUpload delegates to the model-level pure function.
func ValidateImportUpload(mode, sourceType, purpose string) error {
	return model.ValidateImportUpload(mode, sourceType, purpose)
}

// ImportArchiveFormat delegates to the model-level pure function.
func ImportArchiveFormat(filePath string) (contentprofile.ArchiveFormat, string) {
	return model.ImportArchiveFormat(filePath)
}
