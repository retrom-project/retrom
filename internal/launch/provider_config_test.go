package launch

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"retrom/internal/runtimebundle"
)

func TestUnsupportedTargetOptionsAreNotReportedAsInvalidCredentials(t *testing.T) {
	_, err := providerTargetOptions(runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"unknownProperty": map[string]any{"type": "string"}},
		"required":   []any{"unknownProperty"},
	}, providerConfigSource{}, nil)
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

func TestProviderTargetOptionsIncludesValidationRestorePosition(t *testing.T) {
	t.Parallel()
	restoreLaunchID := "10000000-0000-4000-8000-000000000002"
	resume := providerValidationResume{
		RestoreLaunchID: &restoreLaunchID,
		MachineGates: []json.RawMessage{
			json.RawMessage(`{"gate":"SAVE_POINT_RECORDED","status":"PASSED","evidence":{"mapId":1,"playerX":10,"playerY":8,"fixtureState":1}}`),
		},
	}
	position, present, err := providerExpectedRestorePosition(restoreLaunchID, resume)
	if err != nil {
		t.Fatalf("expected restore position: %v", err)
	}
	if !present {
		t.Fatal("expected restore position was not reported as present")
	}
	options, err := providerTargetOptions(runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"expectedRestorePosition": map[string]any{
			"type": []any{"object", "null"}, "additionalProperties": false,
			"properties": map[string]any{
				"fixtureState": map[string]any{"type": "integer", "minimum": int64(0)},
				"mapId":        map[string]any{"type": "integer", "minimum": int64(0)},
				"playerX":      map[string]any{"type": "integer", "minimum": int64(0)},
				"playerY":      map[string]any{"type": "integer", "minimum": int64(0)},
			}, "required": []any{"fixtureState", "mapId", "playerX", "playerY"},
		}}, "required": []any{"expectedRestorePosition"},
	}, providerConfigSource{detectorProfile: "RPG2000"}, &position)
	if err != nil {
		t.Fatalf("RPG Maker target options: %v", err)
	}
	want := map[string]any{
		"expectedRestorePosition": map[string]any{
			"mapId": int64(1), "playerX": int64(10), "playerY": int64(8), "fixtureState": int64(1),
		},
	}
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("RPG Maker target options = %#v, want %#v", options, want)
	}
}

func TestProviderExpectedRestorePositionFailsClosedWithoutSavedGate(t *testing.T) {
	t.Parallel()
	restoreLaunchID := "10000000-0000-4000-8000-000000000002"
	_, _, err := providerExpectedRestorePosition(restoreLaunchID, providerValidationResume{
		RestoreLaunchID: &restoreLaunchID,
		MachineGates:    []json.RawMessage{json.RawMessage(`{"gate":"SAVE_POINT_RECORDED","status":"IN_PROGRESS","evidence":null}`)},
	})
	if err == nil {
		t.Fatal("missing saved position was accepted for a validation restore launch")
	}
}
