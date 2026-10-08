package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"retrom/internal/model"
)

const validSaveMetadata = `{"commitId":"11111111-1111-4111-8111-111111111111","runId":"22222222-2222-4222-8222-222222222222","name":"Checkpoint","kind":"checkpoint","slot":null,"extinfo":{"coreId":"bsnes","providerId":"emulatorjs","targetId":"bsnes","coreFingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","romHash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","checkpointFormat":"bsnes-state-v1-storage-v1","runtimeOptions":{},"content":{"kind":"SINGLE_FILE","entryFile":"game.sfc"}}}`

func TestSaveMetadataUsesRequiredNullableAuthority(t *testing.T) {
	t.Parallel()
	decode, err := saveMetadataValidator()
	if err != nil {
		t.Fatal(err)
	}
	var valid model.SaveInput
	if err = decode(validSaveMetadata, &valid); err != nil || valid.Slot != nil {
		t.Fatalf("explicit null rejected: %v", err)
	}
	for _, field := range []string{"commitId", "runId", "name", "kind", "slot", "extinfo"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(validSaveMetadata), &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, field)
			raw, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			var output model.SaveInput
			if err = decode(string(raw), &output); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("missing required %s accepted: %v", field, err)
			}
		})
	}
}

func TestSaveMetadataRejectsInvalidTypesVersionsAndIdentifiers(t *testing.T) {
	t.Parallel()
	decode, err := saveMetadataValidator()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		field string
		value any
	}{
		{"runId", "not-a-uuid"},
		{"commitId", "not-a-uuid"},
		{"kind", "manual"},
		{"slot", 1},
		{"version", 0},
		{"version", 1.5},
	} {
		t.Run(item.field, func(t *testing.T) {
			t.Parallel()
			var fields map[string]any
			if err := json.Unmarshal([]byte(validSaveMetadata), &fields); err != nil {
				t.Fatal(err)
			}
			fields[item.field] = item.value
			raw, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			var output model.SaveInput
			if err = decode(string(raw), &output); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("accepted invalid metadata: %v", err)
			}
		})
	}
}

func TestQueryRejectsPostgresUnsafeText(t *testing.T) {
	t.Parallel()
	for _, queryString := range []string{"q=a%00b", "q=a%FFb", "platformId=a%00b", "platformId=a%FFb"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games?"+queryString, http.NoBody)
		if _, err := query(r); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("unsafe query %s accepted: %v", queryString, err)
		}
	}
}
