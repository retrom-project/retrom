package testsupport

import (
	"encoding/json"
	"testing"

	"retrom/internal/runtimebundle"
)

// RuntimeEnvelope decodes and closed-validates a launch response for
// integration tests. Assertions intentionally inspect the public Provider
// contract instead of restoring fields from the removed family DTOs.
func RuntimeEnvelope(t testing.TB, value any) map[string]any {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal runtime envelope: %v", err)
	}
	envelope, err := runtimebundle.ParseLaunchEnvelope(contents)
	if err != nil {
		t.Fatalf("parse runtime envelope %s: %v", contents, err)
	}
	return envelope
}

func RuntimeEnvelopeObject(t testing.TB, envelope map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := envelope[key].(map[string]any)
	if !ok {
		t.Fatalf("runtime envelope %s = %#v", key, envelope[key])
	}
	return value
}

func RuntimeEnvelopeResources(t testing.TB, envelope map[string]any, role string) []map[string]any {
	t.Helper()
	items, ok := envelope["resources"].([]any)
	if !ok {
		t.Fatalf("runtime envelope resources = %#v", envelope["resources"])
	}
	resources := make([]map[string]any, 0)
	for _, item := range items {
		resource, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("runtime envelope resource = %#v", item)
		}
		if resource["role"] == role {
			resources = append(resources, resource)
		}
	}
	return resources
}

func RuntimeEnvelopeResource(t testing.TB, envelope map[string]any, role string) map[string]any {
	t.Helper()
	resources := RuntimeEnvelopeResources(t, envelope, role)
	if len(resources) != 1 {
		t.Fatalf("runtime envelope resources for role %q = %#v", role, resources)
	}
	return resources[0]
}

func RuntimeResourceFiles(t testing.TB, resource map[string]any) []map[string]any {
	t.Helper()
	items, ok := resource["files"].([]any)
	if !ok {
		t.Fatalf("runtime resource files = %#v", resource["files"])
	}
	files := make([]map[string]any, 0, len(items))
	for _, item := range items {
		file, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("runtime resource file = %#v", item)
		}
		files = append(files, file)
	}
	return files
}
