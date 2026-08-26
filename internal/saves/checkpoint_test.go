package saves

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/rpgmaker/checkpoint"
)

func TestValidationCheckpointResultJSONIncludesPersistedDigest(t *testing.T) {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	result := ManualResult{
		ResourceKind:  "RPG_RUNTIME_VALIDATION_CHECKPOINT",
		ValidationID:  "01980000-0000-7000-8000-000000000001",
		PayloadKind:   "RUNTIME_STATE",
		SizeBytes:     42,
		PayloadSHA256: digest,
		CreatedAtMS:   1_800_000_000_000,
	}
	contents, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	wanted := `{"resourceKind":"RPG_RUNTIME_VALIDATION_CHECKPOINT","validationId":"01980000-0000-7000-8000-000000000001","payloadKind":"RUNTIME_STATE","nativeProfile":null,"resumeSlot":null,"sizeBytes":42,"sha256":"` + digest + `","createdAtMs":1800000000000}`
	if string(contents) != wanted {
		t.Fatalf("validation checkpoint JSON=%s", contents)
	}
	var replayed ManualResult
	if err := json.Unmarshal(contents, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.PayloadSHA256 != digest || replayed.SizeBytes != result.SizeBytes {
		t.Fatalf("validation checkpoint replay=%#v", replayed)
	}
}

func TestNativeCheckpointBindingDerivesProfileAndSlot(t *testing.T) {
	contents, err := checkpoint.Encode(checkpoint.EngineRPG2003, 100, []checkpoint.Entry{{
		Store: checkpoint.StoreFilesystem, Key: "Save100.lsd",
		MediaType: "application/octet-stream", Data: []byte("map-7-x-4-y-9"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "checkpoint.bin")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	binding, err := validatePayloadBinding(launchSnapshot{
		payloadKind: "NATIVE_SAVE_BUNDLE_V1", generation: "RPG2003",
	}, path)
	if err != nil || binding.profile == nil || *binding.profile != "EASYRPG_V1" ||
		binding.slot == nil || *binding.slot != 100 {
		t.Fatalf("binding=%#v error=%v", binding, err)
	}
	if _, err := validatePayloadBinding(launchSnapshot{
		payloadKind: "NATIVE_SAVE_BUNDLE_V1", generation: "RPG2000",
	}, path); !errors.Is(err, ErrCheckpointInvalid) {
		t.Fatalf("wrong engine error=%v", err)
	}
}

func TestNativeRestoreRejectsManifestDamageAndGenerationDrift(t *testing.T) {
	contents, err := checkpoint.Encode(checkpoint.EngineRPGMZ, 21, []checkpoint.Entry{{
		Store: checkpoint.StoreLocalForage, Key: "file21.rmmzsave",
		MediaType: "application/octet-stream", Data: []byte("saved-position-b"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := validateNativeContents("RPGMZ", contents)
	if err != nil || binding.profile == nil || *binding.profile != "RPGMZ_V1" ||
		binding.slot == nil || *binding.slot != 21 {
		t.Fatalf("binding=%#v error=%v", binding, err)
	}
	if _, err := validateNativeContents("RPGMV", contents); !errors.Is(err, ErrCheckpointIncompatible) {
		t.Fatalf("generation drift error=%v", err)
	}
	damaged := append([]byte(nil), contents...)
	damaged[len(damaged)-1] ^= 0xff
	if _, err := validateNativeContents("RPGMZ", damaged); !errors.Is(err, ErrCheckpointInvalid) {
		t.Fatalf("damaged bundle error=%v", err)
	}
}
