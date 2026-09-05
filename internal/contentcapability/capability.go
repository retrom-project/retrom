package contentcapability

import (
	"encoding/json"
	"slices"

	"retrom/internal/contentprofile"
)

const (
	ModeStandard            = "STANDARD"
	ModeMultiDisc           = "MULTI_DISC"
	ModeRPGMakerProject     = "RPG_MAKER_PROJECT"
	ModeONSProject          = "ONS_PROJECT"
	ModeKiriKiriProject     = "KIRIKIRI_PROJECT"
	ModeButterscotchProject = "BUTTERSCOTCH_PROJECT"
	ModeTyranoScriptProject = "TYRANOSCRIPT_PROJECT"
	DeliveryEagerExternal   = "EAGER_EXTERNAL_FILES"
	MaximumMultiDiscCount   = 8
	MaximumMultiDiscBytes   = int64(1_073_741_824)
)

type MultiDiscLimits struct {
	MaxDiscs      int   `json:"maxDiscs"`
	MaxTotalBytes int64 `json:"maxTotalBytes"`
}

type ImportCapabilities struct {
	ContentModes []string         `json:"contentModes"`
	MultiDisc    *MultiDiscLimits `json:"multiDisc"`
}

type targetPolicy struct {
	SchemaVersion         int      `json:"schemaVersion"`
	SupportedContentKinds []string `json:"supportedContentKinds"`
}

func Resolve(
	platformID string,
	platformInstanceEnabled bool,
	featureEnabled bool,
	compatibilityJSON string,
) ImportCapabilities {
	result := ImportCapabilities{ContentModes: []string{ModeStandard}}
	if project, ok := projectImportCapabilities(platformID, platformInstanceEnabled); ok {
		return project
	}
	if !platformInstanceEnabled || !featureEnabled ||
		!contentprofile.AllowsContentKind(platformID, contentprofile.ContentKindMultiDisc) {
		return result
	}
	var policy targetPolicy
	if json.Unmarshal([]byte(compatibilityJSON), &policy) != nil || policy.SchemaVersion != 1 ||
		!slices.Contains(policy.SupportedContentKinds, ModeMultiDisc) {
		return result
	}
	result.ContentModes = append(result.ContentModes, ModeMultiDisc)
	result.MultiDisc = &MultiDiscLimits{
		MaxDiscs: MaximumMultiDiscCount, MaxTotalBytes: MaximumMultiDiscBytes,
	}
	return result
}

func projectImportCapabilities(platformID string, enabled bool) (ImportCapabilities, bool) {
	if !enabled {
		return ImportCapabilities{}, false
	}
	switch platformID {
	case "rpgmaker":
		return ImportCapabilities{ContentModes: []string{ModeRPGMakerProject}}, true
	case "ons":
		return ImportCapabilities{ContentModes: []string{ModeONSProject}}, true
	case "kirikiri":
		return ImportCapabilities{ContentModes: []string{ModeKiriKiriProject}}, true
	case "butterscotch":
		return ImportCapabilities{ContentModes: []string{ModeButterscotchProject}}, true
	case "tyranoscript":
		return ImportCapabilities{ContentModes: []string{ModeTyranoScriptProject}}, true
	default:
		return ImportCapabilities{}, false
	}
}

// SupportsContentKind is the publication-time capability check. Unlike import
// admission it intentionally does not consult the feature flag, so a frozen
// in-flight review can be completed after admission is closed.
func IsProjectMode(mode string) bool {
	switch mode {
	case ModeRPGMakerProject, ModeONSProject, ModeKiriKiriProject, ModeButterscotchProject, ModeTyranoScriptProject:
		return true
	default:
		return false
	}
}

func SupportsContentKind(compatibilityJSON, contentKind string) bool {
	var policy targetPolicy
	if json.Unmarshal([]byte(compatibilityJSON), &policy) != nil {
		return false
	}
	if policy.SchemaVersion != 1 {
		return false
	}
	switch contentKind {
	case string(contentprofile.ContentKindSingleFile), string(contentprofile.ContentKindDOSBundle):
		return slices.Contains(policy.SupportedContentKinds, contentKind)
	case string(contentprofile.ContentKindMultiDisc):
		return slices.Contains(policy.SupportedContentKinds, contentKind)
	case ModeRPGMakerProject, ModeONSProject, ModeKiriKiriProject,
		ModeButterscotchProject, ModeTyranoScriptProject:
		return slices.Contains(policy.SupportedContentKinds, contentKind)
	default:
		return false
	}
}
