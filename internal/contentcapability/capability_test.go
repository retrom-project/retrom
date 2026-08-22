package contentcapability

import (
	"reflect"
	"testing"

	"retrom/internal/testassert"
)

const saturnCompatibility = `{
  "schemaVersion":5,
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
		{name: "obsolete compatibility", platform: "saturn", instance: true, feature: true, compatibility: `{"schemaVersion":4}`},
		{name: "kind missing", platform: "saturn", instance: true, feature: true, compatibility: `{"schemaVersion":5,"supportedContentKinds":["SINGLE_FILE"],"multiDisc":{"maxDiscs":8,"maxTotalBytes":1073741824,"delivery":"EAGER_EXTERNAL_FILES"}}`},
		{name: "limits invalid", platform: "saturn", instance: true, feature: true, compatibility: `{"schemaVersion":5,"supportedContentKinds":["MULTI_DISC_M3U_V1"],"multiDisc":{"maxDiscs":9,"maxTotalBytes":1073741824,"delivery":"EAGER_EXTERNAL_FILES"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve(test.platform, test.instance, test.feature, test.compatibility)
			if test.wantMulti {
				testassert.Falsef(t, testassert.Any(func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeStandard, ModeMultiDiscM3UV1}) }, func() bool { return got.MultiDisc == nil }, func() bool { return got.MultiDisc.MaxDiscs != 8 }, func() bool { return got.MultiDisc.MaxTotalBytes != MaximumMultiDiscBytes }), "Resolve() = %#v", got)
				return
			}
			testassert.Falsef(t, testassert.Any(func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeStandard}) }, func() bool { return got.MultiDisc != nil }), "Resolve() failed closed = %#v", got)
		})
	}
}

func TestSupportsContentKindRequiresExplicitCompatibilityV3(t *testing.T) {
	t.Parallel()
	standard := `{"schemaVersion":5,"supportedContentKinds":["SINGLE_FILE"]}`
	testassert.False(t, testassert.Any(func() bool { return !SupportsContentKind(standard, "SINGLE_FILE") }, func() bool { return SupportsContentKind(standard, "MULTI_DISC_M3U_V1") }, func() bool { return !SupportsContentKind(saturnCompatibility, "MULTI_DISC_M3U_V1") }, func() bool { return SupportsContentKind(`{"schemaVersion":2}`, "SINGLE_FILE") }, func() bool { return SupportsContentKind(saturnCompatibility, "UNKNOWN") }), "publication capability did not fail closed")
}
