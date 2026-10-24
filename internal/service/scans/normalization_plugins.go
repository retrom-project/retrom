package scans

import "retrom/internal/model"

type normalizationPlugin struct {
	ID         string                    `json:"id"`
	Version    int                       `json:"version"`
	Formats    []string                  `json:"formats"`
	Sources    []normalizationFileDigest `json:"sources"`
	Outputs    []normalizationFileDigest `json:"outputs"`
	Validation normalizationValidation   `json:"validation"`
}

type normalizationFileDigest struct {
	File      string `json:"file"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

type normalizationValidation struct {
	OutputFilesVerified     int64 `json:"outputFilesVerified"`
	ResourceEntriesVerified int64 `json:"resourceEntriesVerified"`
	MetadataCorrections     int64 `json:"metadataCorrections"`
	OmittedPhysicalEntries  int64 `json:"omittedPhysicalEntries"`
}

func validateNormalizationPlugins(plugins []normalizationPlugin) error {
	if len(plugins) == 0 || len(plugins) > 16 {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(plugins))
	for _, plugin := range plugins {
		if !diagnosticCode(plugin.ID) || seen[plugin.ID] || plugin.Version < 1 {
			return model.ErrInvalid
		}
		seen[plugin.ID] = true
		if err := validatePluginFacts(plugin); err != nil {
			return err
		}
	}
	return nil
}

func validatePluginFacts(plugin normalizationPlugin) error {
	if len(plugin.Formats) == 0 || len(plugin.Formats) > 8 {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(plugin.Formats))
	for _, format := range plugin.Formats {
		if !diagnosticCode(format) || seen[format] {
			return model.ErrInvalid
		}
		seen[format] = true
	}
	if !validNormalizationDigests(plugin.Sources) || !validNormalizationDigests(plugin.Outputs) {
		return model.ErrInvalid
	}
	v := plugin.Validation
	if v.OutputFilesVerified != int64(len(plugin.Outputs)) || v.ResourceEntriesVerified < 0 ||
		v.MetadataCorrections < 0 || v.OmittedPhysicalEntries < 0 {
		return model.ErrInvalid
	}
	return nil
}

func validNormalizationDigests(files []normalizationFileDigest) bool {
	if len(files) == 0 || len(files) > maximumGameFiles {
		return false
	}
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		if !diagnosticPath(file.File) || seen[file.File] || !model.Hash(file.SHA256) ||
			file.SizeBytes < 0 || file.SizeBytes > model.MaximumMemberSize {
			return false
		}
		seen[file.File] = true
	}
	return true
}
