package contentcapability

import (
	"encoding/json"
	"slices"

	"retrom/internal/contentprofile"
)

const (
	ModeStandard          = "STANDARD"
	ModeMultiDiscM3UV1    = "MULTI_DISC_M3U_V1"
	DeliveryEagerExternal = "EAGER_EXTERNAL_FILES"
	MaximumMultiDiscCount = 8
	MaximumMultiDiscBytes = int64(1_073_741_824)
)

type MultiDiscLimits struct {
	MaxDiscs      int   `json:"maxDiscs"`
	MaxTotalBytes int64 `json:"maxTotalBytes"`
}

type ImportCapabilities struct {
	ContentModes []string         `json:"contentModes"`
	MultiDisc    *MultiDiscLimits `json:"multiDisc"`
}

type compatibilityV3 struct {
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
	if !platformInstanceEnabled || !featureEnabled ||
		!contentprofile.AllowsContentKind(platformID, contentprofile.ContentKindMultiDiscM3UV1) {
		return result
	}
	var compatibility compatibilityV3
	if json.Unmarshal([]byte(compatibilityJSON), &compatibility) != nil ||
		compatibility.SchemaVersion != 3 || compatibility.MultiDisc == nil ||
		!slices.Contains(compatibility.SupportedContentKinds, ModeMultiDiscM3UV1) ||
		compatibility.MultiDisc.MaxDiscs < 2 || compatibility.MultiDisc.MaxDiscs > MaximumMultiDiscCount ||
		compatibility.MultiDisc.MaxTotalBytes < 1 || compatibility.MultiDisc.MaxTotalBytes > MaximumMultiDiscBytes ||
		compatibility.MultiDisc.Delivery != DeliveryEagerExternal {
		return result
	}
	result.ContentModes = append(result.ContentModes, ModeMultiDiscM3UV1)
	result.MultiDisc = &MultiDiscLimits{
		MaxDiscs: compatibility.MultiDisc.MaxDiscs, MaxTotalBytes: compatibility.MultiDisc.MaxTotalBytes,
	}
	return result
}
