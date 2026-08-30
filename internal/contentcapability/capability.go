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

type compatibility struct {
	SchemaVersion         int      `json:"schemaVersion"`
	SupportedContentKinds []string `json:"supportedContentKinds"`
	MultiDisc             *struct {
		MaxDiscs      int    `json:"maxDiscs"`
		MaxTotalBytes int64  `json:"maxTotalBytes"`
		Delivery      string `json:"delivery"`
	} `json:"multiDisc"`
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
	var compatibility compatibility
	if json.Unmarshal([]byte(compatibilityJSON), &compatibility) != nil || !validMultiDiscCompatibility(compatibility) {
		return result
	}
	result.ContentModes = append(result.ContentModes, ModeMultiDiscM3UV1)
	result.MultiDisc = &MultiDiscLimits{
		MaxDiscs: compatibility.MultiDisc.MaxDiscs, MaxTotalBytes: compatibility.MultiDisc.MaxTotalBytes,
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
	default:
		return ImportCapabilities{}, false
	}
}

func validMultiDiscCompatibility(value compatibility) bool {
	return value.SchemaVersion == 5 && value.MultiDisc != nil &&
		slices.Contains(value.SupportedContentKinds, ModeMultiDiscM3UV1) &&
		value.MultiDisc.MaxDiscs >= 2 && value.MultiDisc.MaxDiscs <= MaximumMultiDiscCount &&
		value.MultiDisc.MaxTotalBytes >= 1 && value.MultiDisc.MaxTotalBytes <= MaximumMultiDiscBytes &&
		value.MultiDisc.Delivery == DeliveryEagerExternal
}

// SupportsContentKind is the publication-time capability check. Unlike import
// admission it intentionally does not consult the feature flag, so a frozen
// in-flight review can be completed after admission is closed.
func SupportsContentKind(compatibilityJSON, contentKind string) bool {
	if allowedAdapterABIs := projectAdapterABIs(contentKind); allowedAdapterABIs != nil {
		var projectCompatibility struct {
			AdapterABI string `json:"adapterAbi"`
		}
		if json.Unmarshal([]byte(compatibilityJSON), &projectCompatibility) != nil {
			return false
		}
		return slices.Contains(allowedAdapterABIs, projectCompatibility.AdapterABI)
	}
	var compatibility compatibility
	if json.Unmarshal([]byte(compatibilityJSON), &compatibility) != nil ||
		compatibility.SchemaVersion != 5 ||
		!slices.Contains(compatibility.SupportedContentKinds, contentKind) {
		return false
	}
	switch contentKind {
	case string(contentprofile.ContentKindSingleFile), string(contentprofile.ContentKindDOSBundle):
		return true
	case string(contentprofile.ContentKindMultiDiscM3UV1):
		return compatibility.MultiDisc != nil &&
			compatibility.MultiDisc.MaxDiscs >= 2 &&
			compatibility.MultiDisc.MaxDiscs <= MaximumMultiDiscCount &&
			compatibility.MultiDisc.MaxTotalBytes >= 1 &&
			compatibility.MultiDisc.MaxTotalBytes <= MaximumMultiDiscBytes &&
			compatibility.MultiDisc.Delivery == DeliveryEagerExternal
	default:
		return false
	}
}

func projectAdapterABIs(contentKind string) []string {
	switch contentKind {
	case ModeRPGMakerProjectV1:
		return []string{"easyrpg-save", "mkxp-state-compact", "native-save"}
	case ModeONSProjectV1:
		return []string{"ons-save"}
	case ModeKiriKiriProjectV1:
		return []string{"kirikiri-kag-bookmark"}
	case ModeButterscotchProjectV1:
		return []string{"butterscotch-checkpoint-v2"}
	default:
		return nil
	}
}
