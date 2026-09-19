package launch

import (
	json "encoding/json"
	"errors"
	"strings"
	"testing"

	"retrom/internal/capability/content/contentprofile"
	model "retrom/internal/model/launch"
	runtimecontract "retrom/internal/model/runtimecontract"
)

func TestUnsupportedTargetOptionsAreNotReportedAsInvalidCredentials(t *testing.T) {
	_, err := providerTargetOptions(runtimecontract.TargetOptionsSchema{
		"type": json.RawMessage("\"object\""), "additionalProperties": json.RawMessage("false"),
		"properties": json.RawMessage("{\"unknownProperty\":{\"type\":\"string\"}}"),
		"required":   json.RawMessage("[\"unknownProperty\"]"),
	}, model.ConfigSource{})
	if err == nil || errors.Is(err, model.ErrCredential) {
		t.Fatalf("unsupported options misclassified as authentication: %v", err)
	}
}

func TestProviderBlobResourcePublishesMaterializedMKXPArchive(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	contract := strings.Repeat("b", 64)
	resource, err := providerSeekableProjectResource(contract, []model.ConfigFile{{
		LogicalName: MKXPArchiveName, Format: string(contentprofile.ContentKindRPGMakerProject),
		Digest: digest, Size: 1024,
	}})
	if err != nil {
		t.Fatalf("materialized MKXP resource: %v", err)
	}
	url, ok := resource["url"].(string)
	if !ok || url != RuntimeProjectContentPrefix+contract+"/"+MKXPArchivePublicName {
		t.Fatalf("materialized MKXP resource URL = %#v", resource["url"])
	}
}

func TestRPGTargetOptionsDoNotDependOnReviewProof(t *testing.T) {
	t.Parallel()
	options, err := providerTargetOptions(runtimecontract.TargetOptionsSchema{
		"type": json.RawMessage("\"object\""), "additionalProperties": json.RawMessage("false"),
		"properties": json.RawMessage("{}"), "required": json.RawMessage("[]"),
	}, model.ConfigSource{DetectorProfile: "RPG2000"})
	if err != nil || string(options) != "{}" {
		t.Fatalf("ordinary RPG launch requires review proof: options=%v error=%v", options, err)
	}
}
