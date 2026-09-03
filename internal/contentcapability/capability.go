package contentcapability

import (
	"encoding/json"
	"slices"

	"retrom/internal/contentprofile"
)

const (
	ModeStandard              = "STANDARD"
	ModeMultiDiscM3UV1        = "MULTI_DISC_M3U_V1"
	ModeRPGMakerProjectV1     = "RPG_MAKER_PROJECT_V1"
	ModeONSProjectV1          = "ONS_PROJECT_V1"
	ModeKiriKiriProjectV1     = "KIRIKIRI_PROJECT_V1"
	ModeButterscotchProjectV1 = "BUTTERSCOTCH_PROJECT_V1"
	ModeTyranoScriptProjectV1 = "TYRANOSCRIPT_PROJECT_V1"
	DeliveryEagerExternal     = "EAGER_EXTERNAL_FILES"
	MaximumMultiDiscCount     = 8
	MaximumMultiDiscBytes     = int64(1_073_741_824)
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
		!contentprofile.AllowsContentKind(platformID, contentprofile.ContentKindMultiDiscM3UV1) {
		return result
	}
	var policy targetPolicy
	if json.Unmarshal([]byte(compatibilityJSON), &policy) != nil || policy.SchemaVersion != 1 ||
		!slices.Contains(policy.SupportedContentKinds, ModeMultiDiscM3UV1) {
		return result
	}
	result.ContentModes = append(result.ContentModes, ModeMultiDiscM3UV1)
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
		return ImportCapabilities{ContentModes: []string{ModeRPGMakerProjectV1}}, true
	case "ons":
		return ImportCapabilities{ContentModes: []string{ModeONSProjectV1}}, true
	case "kirikiri":
		return ImportCapabilities{ContentModes: []string{ModeKiriKiriProjectV1}}, true
	case "butterscotch":
		return ImportCapabilities{ContentModes: []string{ModeButterscotchProjectV1}}, true
	case "tyranoscript":
		return ImportCapabilities{ContentModes: []string{ModeTyranoScriptProjectV1}}, true
	default:
		return ImportCapabilities{}, false
	}
}

// SupportsContentKind is the publication-time capability check. Unlike import
// admission it intentionally does not consult the feature flag, so a frozen
// in-flight review can be completed after admission is closed.
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
	case string(contentprofile.ContentKindMultiDiscM3UV1):
		return slices.Contains(policy.SupportedContentKinds, contentKind)
	case ModeRPGMakerProjectV1, ModeONSProjectV1, ModeKiriKiriProjectV1,
		ModeButterscotchProjectV1, ModeTyranoScriptProjectV1:
		return slices.Contains(policy.SupportedContentKinds, contentKind)
	default:
		return false
	}
}
