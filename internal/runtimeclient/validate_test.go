package runtimeclient

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

type identityHost struct{}

func (identityHost) Call(_ context.Context, command string, input []byte) (json.RawMessage, error) {
	var request struct {
		Items []struct {
			Directory map[string]any `json:"directory"`
		} `json:"items"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, err
	}
	valid := command == "batch-identity" && len(request.Items) == 1 && reflect.DeepEqual(request.Items[0].Directory,
		map[string]any{
			"platformId": "platform", "defaultCoreId": "multi-target",
			"allowedCoreIds": []any{"multi-target", "another-core"},
		})
	if valid {
		return json.RawMessage(`[{"error":""}]`), nil
	}
	return json.RawMessage(`[{"error":"RUNTIME_CORE_UNAVAILABLE"}]`), nil
}

func TestValidatePassesEachPlatformCoreOnceAcrossMultipleTargets(t *testing.T) {
	pool := newHostPool(t.Context(), identityHost{})
	t.Cleanup(pool.Close)
	client := &Client{pool: pool, Bindings: []Binding{
		{CoreID: "unrelated", PlatformIDs: []string{"another-platform"}},
		{CoreID: "multi-target", TargetID: "first", PlatformIDs: []string{"platform"}},
		{CoreID: "multi-target", TargetID: "second", PlatformIDs: []string{"platform"}},
		{CoreID: "another-core", PlatformIDs: []string{"platform"}},
	}}
	if err := client.Validate(t.Context(), "platform", json.RawMessage(`{}`), nil); err != nil {
		t.Fatalf("validating a core shared by multiple targets: %v", err)
	}
}
