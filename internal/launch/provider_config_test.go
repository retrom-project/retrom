package launch

import (
	"errors"
	"strings"
	"testing"

	"retrom/internal/runtimebundle"
)

func TestUnsupportedTargetOptionsAreNotReportedAsInvalidCredentials(t *testing.T) {
	_, err := providerTargetOptions(runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"unknownProperty": map[string]any{"type": "string"}},
		"required":   []any{"unknownProperty"},
	}, providerConfigSource{})
	if err == nil || errors.Is(err, ErrCredential) {
		t.Fatalf("unsupported options misclassified as authentication: %v", err)
	}
}

func TestProviderBlobResourcePublishesMaterializedMKXPArchive(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	contract := strings.Repeat("b", 64)
	resource, err := providerSeekableProjectResource(contract, []lockedProviderFile{{
		logicalName: rpgMKXPArchiveName, format: rpgProjectFormat,
		digest: digest, size: 1024,
	}})
	if err != nil {
		t.Fatalf("materialized MKXP resource: %v", err)
	}
	url, ok := resource["url"].(string)
	if !ok || url != RuntimeProjectContentPrefix+contract+"/"+rpgMKXPArchivePublicName {
		t.Fatalf("materialized MKXP resource URL = %#v", resource["url"])
	}
}

func TestRPGTargetOptionsDoNotDependOnReviewProof(t *testing.T) {
	t.Parallel()
	options, err := providerTargetOptions(runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{}, "required": []any{},
	}, providerConfigSource{detectorProfile: "RPG2000"})
	if err != nil || len(options) != 0 {
		t.Fatalf("ordinary RPG launch requires review proof: options=%v error=%v", options, err)
	}
}
