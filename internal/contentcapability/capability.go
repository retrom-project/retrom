package contentcapability

import (
	"retrom/internal/contentprofile"
)

const (
	ModeStandard            = "STANDARD"
	ModeMultiDisc           = string(contentprofile.ContentKindMultiDisc)
	ModeRPGMakerProject     = string(contentprofile.ContentKindRPGMakerProject)
	ModeONSProject          = string(contentprofile.ContentKindONSProject)
	ModeKiriKiriProject     = string(contentprofile.ContentKindKiriKiriProject)
	ModeButterscotchProject = string(contentprofile.ContentKindButterscotchProject)
	ModeTyranoScriptProject = string(contentprofile.ContentKindTyranoScriptProject)
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

func Resolve(
	platformID string,
	platformInstanceEnabled bool,
	featureEnabled bool,
	policy Policy,
) ImportCapabilities {
	result := ImportCapabilities{ContentModes: []string{ModeStandard}}
	if project, ok := projectImportCapabilities(platformID, platformInstanceEnabled); ok {
		return project
	}
	if !platformInstanceEnabled || !featureEnabled ||
		!contentprofile.AllowsContentKind(platformID, contentprofile.ContentKindMultiDisc) {
		return result
	}
	if !policy.Supports(ModeMultiDisc) || policy.MultiDisc == nil {
		return result
	}
	result.ContentModes = append(result.ContentModes, ModeMultiDisc)
	limits := policy.MultiDisc.MultiDiscLimits
	result.MultiDisc = &limits
	return result
}

func projectImportCapabilities(platformID string, enabled bool) (ImportCapabilities, bool) {
	kind, ok := contentprofile.ProjectKind(platformID)
	if !enabled || !ok {
		return ImportCapabilities{}, false
	}
	return ImportCapabilities{ContentModes: []string{string(kind)}}, true
}

func IsProjectMode(mode string) bool {
	return contentprofile.IsProjectContentKind(contentprofile.ContentKind(mode))
}
