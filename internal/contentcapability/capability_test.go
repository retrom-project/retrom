package contentcapability

import (
	"reflect"
	"testing"

	"retrom/internal/testassert"
)

var saturnTargetPolicy = NewPolicy("MULTI_DISC", "SINGLE_FILE")

func TestResolveRequiresFeaturePlatformInstanceAndArtifactIntersection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		platform  string
		instance  bool
		feature   bool
		policy    Policy
		wantMulti bool
	}{
		{name: "saturn intersection", platform: "saturn", instance: true, feature: true, policy: saturnTargetPolicy, wantMulti: true},
		{name: "feature disabled", platform: "saturn", instance: true, policy: saturnTargetPolicy},
		{name: "instance disabled", platform: "saturn", feature: true, policy: saturnTargetPolicy},
		{name: "platform unsupported", platform: "psx", instance: true, feature: true, policy: saturnTargetPolicy},
		{name: "binding unavailable", platform: "saturn", instance: true, feature: true, policy: Policy{}},
		{name: "kind missing", platform: "saturn", instance: true, feature: true, policy: NewPolicy("SINGLE_FILE")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve(test.platform, test.instance, test.feature, test.policy)
			if test.wantMulti {
				testassert.Falsef(t, testassert.Any(func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeStandard, ModeMultiDisc}) }, func() bool { return got.MultiDisc == nil }, func() bool { return got.MultiDisc.MaxDiscs != 8 }, func() bool { return got.MultiDisc.MaxTotalBytes != MaximumMultiDiscBytes }), "Resolve() = %#v", got)
				return
			}
			testassert.Falsef(t, testassert.Any(func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeStandard}) }, func() bool { return got.MultiDisc != nil }), "Resolve() failed closed = %#v", got)
		})
	}
}

func TestResolveRPGMakerUsesOnlyProjectMode(t *testing.T) {
	t.Parallel()
	got := Resolve("rpgmaker", true, false, Policy{})
	testassert.Falsef(t, testassert.Any(
		func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeRPGMakerProject}) },
		func() bool { return got.MultiDisc != nil },
	), "RPG Maker capabilities = %#v", got)

	disabled := Resolve("rpgmaker", false, true, Policy{})
	testassert.Falsef(t, !reflect.DeepEqual(disabled.ContentModes, []string{ModeStandard}), "disabled RPG Maker capabilities = %#v", disabled)
}

func TestResolveONSUsesOnlyProjectMode(t *testing.T) {
	t.Parallel()
	got := Resolve("ons", true, false, Policy{})
	testassert.Falsef(t, testassert.Any(
		func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeONSProject}) },
		func() bool { return got.MultiDisc != nil },
	), "ONS capabilities = %#v", got)
	if !NewPolicy("ONS_PROJECT").Supports(ModeONSProject) ||
		NewPolicy("SINGLE_FILE").Supports(ModeONSProject) {
		t.Fatal("ONS publication capability did not enforce Host binding")
	}
}

func TestResolveButterscotchUsesOnlyProjectMode(t *testing.T) {
	t.Parallel()
	got := Resolve("butterscotch", true, false, Policy{})
	testassert.Falsef(t, testassert.Any(
		func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeButterscotchProject}) },
		func() bool { return got.MultiDisc != nil },
	), "Butterscotch capabilities=%#v", got)
	if !NewPolicy("BUTTERSCOTCH_PROJECT").Supports(ModeButterscotchProject) ||
		NewPolicy("SINGLE_FILE").Supports(ModeButterscotchProject) {
		t.Fatal("Butterscotch publication capability did not enforce Host binding")
	}
}

func TestResolveTyranoScriptUsesOnlyProjectMode(t *testing.T) {
	t.Parallel()
	got := Resolve("tyranoscript", true, false, Policy{})
	testassert.Falsef(t, testassert.Any(
		func() bool { return !reflect.DeepEqual(got.ContentModes, []string{ModeTyranoScriptProject}) },
		func() bool { return got.MultiDisc != nil },
	), "TyranoScript capabilities=%#v", got)
	if !NewPolicy("TYRANOSCRIPT_PROJECT").Supports(ModeTyranoScriptProject) ||
		NewPolicy("SINGLE_FILE").Supports(ModeTyranoScriptProject) {
		t.Fatal("TyranoScript publication capability did not enforce Host binding")
	}
}

func TestSupportsContentKindRequiresExplicitProviderInputs(t *testing.T) {
	t.Parallel()
	standard := NewPolicy("SINGLE_FILE")
	testassert.False(t, testassert.Any(
		func() bool { return !standard.Supports("SINGLE_FILE") },
		func() bool { return standard.Supports("MULTI_DISC") },
		func() bool { return !saturnTargetPolicy.Supports("MULTI_DISC") },
		func() bool {
			return NewPolicy().Supports("SINGLE_FILE")
		},
		func() bool { return saturnTargetPolicy.Supports("UNKNOWN") },
		func() bool {
			return !NewPolicy("RPG_MAKER_PROJECT").Supports(ModeRPGMakerProject)
		},
		func() bool {
			return standard.Supports(ModeRPGMakerProject)
		},
		func() bool { return (Policy{}).Supports(ModeRPGMakerProject) },
	), "publication capability did not fail closed against the Host binding")
}
