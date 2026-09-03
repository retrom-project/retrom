package saves

import (
	"encoding/json"
	"testing"
)

func TestValidationCheckpointResultJSONUsesOpaqueProviderFormat(t *testing.T) {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	result := ManualResult{
		ResourceKind:     "RPG_RUNTIME_VALIDATION_CHECKPOINT",
		ValidationID:     "01980000-0000-7000-8000-000000000001",
		CheckpointFormat: "provider-checkpoint-v1",
		SizeBytes:        42,
		PayloadSHA256:    digest,
		CreatedAtMS:      1_800_000_000_000,
	}
	contents, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	wanted := `{"resourceKind":"RPG_RUNTIME_VALIDATION_CHECKPOINT","validationId":"01980000-0000-7000-8000-000000000001","checkpointFormat":"provider-checkpoint-v1","sizeBytes":42,"sha256":"` + digest + `","createdAtMs":1800000000000}`
	if string(contents) != wanted {
		t.Fatalf("validation checkpoint JSON=%s", contents)
	}
	var replayed ManualResult
	if err := json.Unmarshal(contents, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.CheckpointFormat != result.CheckpointFormat || replayed.PayloadSHA256 != digest ||
		replayed.SizeBytes != result.SizeBytes {
		t.Fatalf("validation checkpoint replay=%#v", replayed)
	}
}

func TestCheckpointMetadataAcceptsOnlyLaunchWriteFormat(t *testing.T) {
	launch := launchSnapshot{purpose: "PRODUCT", checkpointFormat: "opaque-v2"}
	if !validMetadataForLaunch(manualMetadata{CheckpointFormat: "opaque-v2", Name: "slot"}, launch) {
		t.Fatal("provider write format should be accepted without inspecting its payload")
	}
	if validMetadataForLaunch(manualMetadata{CheckpointFormat: "opaque-v1", Name: "slot"}, launch) {
		t.Fatal("a format other than the target write format must be rejected")
	}
}
