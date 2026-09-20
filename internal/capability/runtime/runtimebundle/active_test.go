package runtimebundle

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseActiveDescriptorClosesCandidateIdentity(t *testing.T) {
	value, err := ParseActiveDescriptor([]byte(validCandidateActive))
	if err != nil {
		t.Fatal(err)
	}
	if value.Source != "candidate" || value.Release != nil || len(value.Providers) != 1 ||
		value.Providers[0].Targets[0].Checkpoint.ReadFormats[0] != "fixture-state-v1" {
		t.Fatalf("unexpected active descriptor: %#v", value)
	}
	for _, mutation := range []string{
		strings.Replace(validCandidateActive, `"source":"candidate"`, `"source":"production"`, 1),
		strings.Replace(validCandidateActive, `"schemaVersion":1`, `"schemaVersion":1,"extra":true`, 1),
		strings.Replace(validCandidateActive, `"providerApiVersion":1`, `"providerApiVersion":2`, 1),
		strings.Replace(validCandidateActive, `"providerId":"fixture"`, `"providerId":"fixture","providerId":"other"`, 1),
	} {
		if _, err := ParseActiveDescriptor([]byte(mutation)); !errors.Is(err, ErrActiveInvalid) {
			t.Fatalf("invalid active descriptor accepted: %v", err)
		}
	}
}

func TestProductionProviderVersionsFollowReleaseTag(t *testing.T) {
	value, err := ParseActiveDescriptor([]byte(validCandidateActive))
	if err != nil {
		t.Fatal(err)
	}
	value.Source = "production"
	value.SourceTreeSHA256 = nil
	value.Release = &ReleaseIdentity{Repository: providerRepository, Tag: "v0.46.0", Commit: strings.Repeat("a", 40)}
	for _, version := range []string{"0.46.0", "2.21.0", "0.45.0", "0.0.0-dev"} {
		value.Providers[0].ProviderVersion = version
		contents, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		_, parseErr := ParseActiveDescriptor(contents)
		if version == "0.46.0" && parseErr != nil || version != "0.46.0" && !errors.Is(parseErr, ErrActiveInvalid) {
			t.Fatalf("version %s: %v", version, parseErr)
		}
	}
}

const validCandidateActive = `{
  "providers":[{
    "bundleSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "bundleSizeBytes":100,
    "clientModulePath":"client.mjs",
    "fileCount":6,
    "installationPath":"fixture/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "manifestSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "moduleSha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
    "providerApiVersion":1,
    "providerId":"fixture",
    "providerVersion":"1.0.0",
    "targets":[{
      "checkpoint":{"maxBytes":1024,"readFormats":["fixture-state-v1"],"writeFormat":"fixture-state-v1"},
      "id":"fixture"
    }],
    "unpackedSizeBytes":200
  }],
  "release":null,
  "schemaVersion":1,
  "source":"candidate",
  "sourceTreeSha256":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
}`
