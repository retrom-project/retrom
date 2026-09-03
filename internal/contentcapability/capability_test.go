package contentcapability

import (
	"reflect"
	"testing"

	"retrom/internal/testassert"
)

const saturnTargetPolicy = `{"schemaVersion":1,"supportedContentKinds":["MULTI_DISC_M3U_V1","SINGLE_FILE"]}`

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
		{name: "saturn intersection", platform: "saturn", instance: true, feature: true, compatibility: saturnTargetPolicy, wantMulti: true},
		{name: "feature disabled", platform: "saturn", instance: true, compatibility: saturnTargetPolicy},
		{name: "instance disabled", platform: "saturn", feature: true, compatibility: saturnTargetPolicy},
		{name: "platform unsupported", platform: "psx", instance: true, feature: true, compatibility: saturnTargetPolicy},
		{name: "invalid target fragment", platform: "saturn", instance: true, feature: true, compatibility: `{"inputs":true}`},
		{name: "kind missing", platform: "saturn", instance: true, feature: true, compatibility: `{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"]}`},
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

func TestResolveRPGMakerUsesOnlyProjectMode(t *testing.T) {
	t.Parallel()
	got := Resolve("rpgmaker", true, false, `{}`)
	testassert.Falsef(t, testassert.Any(
		func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeRPGMakerProjectV1}) },
		func() bool { return got.MultiDisc != nil },
	), "RPG Maker capabilities = %#v", got)

	disabled := Resolve("rpgmaker", false, true, `{}`)
	testassert.Falsef(t, !reflect.DeepEqual(disabled.ContentModes, []string{ModeStandard}), "disabled RPG Maker capabilities = %#v", disabled)
}

func TestResolveONSUsesOnlyProjectMode(t *testing.T) {
	t.Parallel()
	got := Resolve("ons", true, false, `{}`)
	testassert.Falsef(t, testassert.Any(
		func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeONSProjectV1}) },
		func() bool { return got.MultiDisc != nil },
	), "ONS capabilities = %#v", got)
	if !SupportsContentKind(`{"schemaVersion":1,"supportedContentKinds":["ONS_PROJECT_V1"]}`, ModeONSProjectV1) ||
		SupportsContentKind(`{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"]}`, ModeONSProjectV1) {
		t.Fatal("ONS publication capability did not enforce Host binding")
	}
}

func TestResolveButterscotchUsesOnlyProjectMode(t *testing.T) {
	t.Parallel()
	got := Resolve("butterscotch", true, false, `{}`)
	testassert.Falsef(t, testassert.Any(
		func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeButterscotchProjectV1}) },
		func() bool { return got.MultiDisc != nil },
	), "Butterscotch capabilities=%#v", got)
	if !SupportsContentKind(`{"schemaVersion":1,"supportedContentKinds":["BUTTERSCOTCH_PROJECT_V1"]}`, ModeButterscotchProjectV1) ||
		SupportsContentKind(`{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"]}`, ModeButterscotchProjectV1) {
		t.Fatal("Butterscotch publication capability did not enforce Host binding")
	}
}

func TestResolveTyranoScriptUsesOnlyProjectMode(t *testing.T) {
	t.Parallel()
	got := Resolve("tyranoscript", true, false, `{}`)
	testassert.Falsef(t, testassert.Any(
		func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeTyranoScriptProjectV1}) },
		func() bool { return got.MultiDisc != nil },
	), "TyranoScript capabilities=%#v", got)
	if !SupportsContentKind(`{"schemaVersion":1,"supportedContentKinds":["TYRANOSCRIPT_PROJECT_V1"]}`, ModeTyranoScriptProjectV1) ||
		SupportsContentKind(`{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"]}`, ModeTyranoScriptProjectV1) {
		t.Fatal("TyranoScript publication capability did not enforce Host binding")
	}
}

func TestSupportsContentKindRequiresExplicitProviderInputs(t *testing.T) {
	t.Parallel()
	standard := `{"schemaVersion":1,"supportedContentKinds":["SINGLE_FILE"]}`
	testassert.False(t, testassert.Any(
		func() bool { return !SupportsContentKind(standard, "SINGLE_FILE") },
		func() bool { return SupportsContentKind(standard, "MULTI_DISC_M3U_V1") },
		func() bool { return !SupportsContentKind(saturnTargetPolicy, "MULTI_DISC_M3U_V1") },
		func() bool {
			return SupportsContentKind(`{"schemaVersion":1,"supportedContentKinds":[]}`, "SINGLE_FILE")
		},
		func() bool { return SupportsContentKind(saturnTargetPolicy, "UNKNOWN") },
		func() bool {
			return !SupportsContentKind(`{"schemaVersion":1,"supportedContentKinds":["RPG_MAKER_PROJECT_V1"]}`, ModeRPGMakerProjectV1)
		},
		func() bool {
			return SupportsContentKind(standard, ModeRPGMakerProjectV1)
		},
		func() bool { return SupportsContentKind(`not-json`, ModeRPGMakerProjectV1) },
	), "publication capability did not fail closed against the Host binding")
}
