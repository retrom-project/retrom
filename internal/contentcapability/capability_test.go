package contentcapability

import (
	"reflect"
	"testing"
)

const saturnCompatibility = `{
  "schemaVersion":3,
  "supportedContentKinds":["SINGLE_FILE","MULTI_DISC_M3U_V1"],
  "multiDisc":{"maxDiscs":8,"maxTotalBytes":1073741824,"delivery":"EAGER_EXTERNAL_FILES"}
}`

func TestResolveRequiresFeaturePlatformInstanceAndArtifactIntersection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		platform      string
		instance      bool
		feature       bool
		compatibility string
		wantMulti     bool
	}{
		{name: "saturn intersection", platform: "saturn", instance: true, feature: true, compatibility: saturnCompatibility, wantMulti: true},
		{name: "feature disabled", platform: "saturn", instance: true, compatibility: saturnCompatibility},
		{name: "instance disabled", platform: "saturn", feature: true, compatibility: saturnCompatibility},
		{name: "platform unsupported", platform: "psx", instance: true, feature: true, compatibility: saturnCompatibility},
		{name: "legacy compatibility", platform: "saturn", instance: true, feature: true, compatibility: `{"schemaVersion":2}`},
		{name: "kind missing", platform: "saturn", instance: true, feature: true, compatibility: `{"schemaVersion":3,"supportedContentKinds":["SINGLE_FILE"],"multiDisc":{"maxDiscs":8,"maxTotalBytes":1073741824,"delivery":"EAGER_EXTERNAL_FILES"}}`},
		{name: "limits invalid", platform: "saturn", instance: true, feature: true, compatibility: `{"schemaVersion":3,"supportedContentKinds":["MULTI_DISC_M3U_V1"],"multiDisc":{"maxDiscs":9,"maxTotalBytes":1073741824,"delivery":"EAGER_EXTERNAL_FILES"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve(test.platform, test.instance, test.feature, test.compatibility)
			if test.wantMulti {
				if !reflect.DeepEqual(got.ContentModes, []string{ModeStandard, ModeMultiDiscM3UV1}) ||
					got.MultiDisc == nil || got.MultiDisc.MaxDiscs != 8 || got.MultiDisc.MaxTotalBytes != MaximumMultiDiscBytes {
					t.Fatalf("Resolve() = %#v", got)
				}
				return
			}
			if !reflect.DeepEqual(got.ContentModes, []string{ModeStandard}) || got.MultiDisc != nil {
				t.Fatalf("Resolve() failed closed = %#v", got)
			}
		})
	}
}
