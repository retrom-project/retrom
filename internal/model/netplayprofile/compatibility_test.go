package netplayprofile

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRegistryValueCompatibilityGolden(t *testing.T) {
	t.Parallel()
	_, registry, golden := compatibilityRegistry(t)
	if registry.ManifestDigest != golden.RawManifestSHA256 {
		t.Fatalf("manifest digest = %s, want %s", registry.ManifestDigest, golden.RawManifestSHA256)
	}
	for _, value := range []struct {
		name  string
		input any
		want  string
	}{
		{"protocol", registry.Manifest.Protocol, golden.ProtocolJSON},
		{"manifest", registry.Manifest, golden.ManifestJSON},
	} {
		actual, err := json.Marshal(value.input)
		if err != nil || !bytes.Equal(actual, []byte(value.want)) {
			t.Fatalf("%s JSON = %s, want %s, error = %v", value.name, actual, value.want, err)
		}
	}
	if len(golden.Profiles) != 8 || len(registry.Profiles()) != len(golden.Profiles) {
		t.Fatalf("profile counts = %d / %d, want 8", len(golden.Profiles), len(registry.Profiles()))
	}
	for _, expected := range golden.Profiles {
		t.Run(expected.ProfileID, func(t *testing.T) {
			entry, ok := registry.Profile(expected.ProfileID)
			if !ok {
				t.Fatal("registered profile missing")
			}
			actual, digest, err := registry.CanonicalProfile(CanonicalProfileInput{
				ManifestProfile: entry, BundleSHA256: strings.Repeat("a", 64),
				SourceManifestDigest: strings.Repeat("b", 64), DependencySnapshotJSON: "{\"schemaVersion\":1}",
			})
			if err != nil || !bytes.Equal(actual, []byte(expected.CanonicalJSON)) || digest != expected.CanonicalSHA256 {
				t.Fatalf("canonical = %s / %s, want %s / %s, error = %v", actual, digest, expected.CanonicalJSON, expected.CanonicalSHA256, err)
			}
		})
	}
}

func TestRegistryKeepsRawDigestInputs(t *testing.T) {
	t.Parallel()
	raw, registry, golden := compatibilityRegistry(t)
	whitespace, err := ParseRegistry(append(bytes.Clone(raw), '\n'), fixtureBindings())
	if err != nil {
		t.Fatal(err)
	}
	if whitespace.ManifestDigest == registry.ManifestDigest {
		t.Fatal("manifest whitespace disappeared from its source digest")
	}
	entry, ok := whitespace.Profile(golden.Profiles[0].ProfileID)
	if !ok {
		t.Fatal("registered profile missing")
	}
	input := CanonicalProfileInput{
		ManifestProfile: entry, BundleSHA256: strings.Repeat("a", 64),
		SourceManifestDigest: strings.Repeat("b", 64), DependencySnapshotJSON: "{\"schemaVersion\":1}",
	}
	actual, digest, err := whitespace.CanonicalProfile(input)
	if err != nil || string(actual) != golden.Profiles[0].CanonicalJSON || digest != golden.Profiles[0].CanonicalSHA256 {
		t.Fatalf("manifest-only whitespace changed canonical: %s / %s, error = %v", actual, digest, err)
	}
	input.DependencySnapshotJSON += " "
	_, digest, err = registry.CanonicalProfile(input)
	if err != nil || digest == golden.Profiles[0].CanonicalSHA256 {
		t.Fatalf("raw dependency whitespace not bound: %s, error = %v", digest, err)
	}
}

func TestCanonicalProfilePreservesCallerFieldsAndErrorIdentity(t *testing.T) {
	t.Parallel()
	_, registry, golden := compatibilityRegistry(t)
	entry, ok := registry.Profile(golden.Profiles[0].ProfileID)
	if !ok {
		t.Fatal("registered profile missing")
	}
	input := CanonicalProfileInput{
		ManifestProfile: entry, BundleSHA256: strings.Repeat("a", 64),
		SourceManifestDigest: strings.Repeat("b", 64), DependencySnapshotJSON: "{\"schemaVersion\":1}",
	}
	input.CoreID = "caller-core"
	actual, _, err := registry.CanonicalProfile(input)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct{ CoreID string }
	if err := json.Unmarshal(actual, &projected); err != nil {
		t.Fatal(err)
	}
	if projected.CoreID != input.CoreID {
		t.Fatalf("caller core overwritten: %s", actual)
	}
	for _, test := range []struct {
		name   string
		change func(*CanonicalProfileInput)
	}{
		{"unregistered profile", func(value *CanonicalProfileInput) { value.ID = "unregistered" }},
		{"uppercase bundle", func(value *CanonicalProfileInput) { value.BundleSHA256 = strings.Repeat("A", 64) }},
		{"invalid source digest", func(value *CanonicalProfileInput) { value.SourceManifestDigest = strings.Repeat("g", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := input
			test.change(&invalid)
			data, digest, err := registry.CanonicalProfile(invalid)
			if !errors.Is(err, ErrManifestInvalid) || errors.Unwrap(err) != nil || data != nil || digest != "" {
				t.Fatalf("invalid canonical result = %s / %s / %v", data, digest, err)
			}
		})
	}
}
