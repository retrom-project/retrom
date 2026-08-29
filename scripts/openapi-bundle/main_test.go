package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestBundleInternalizesLocalReferencesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "openapi.yaml", rootFixture("./domains/health.yaml"))
	writeFixture(t, root, "domains/health.yaml", healthFixture())
	first := filepath.Join(root, "first.json")
	second := filepath.Join(root, "second.json")

	if err := bundle(filepath.Join(root, "openapi.yaml"), first); err != nil {
		t.Fatal(err)
	}
	if err := bundle(filepath.Join(root, "openapi.yaml"), second); err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("bundled output is not deterministic")
	}
	if strings.Contains(string(firstBytes), "domains/health.yaml") {
		t.Fatal("bundled output retained an external reference")
	}
	var document map[string]any
	if err := yaml.Unmarshal(firstBytes, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	health := paths["/health"].(map[string]any)
	get := health["get"].(map[string]any)
	responses := get["responses"].(map[string]any)
	response := responses["200"].(map[string]any)
	if response["$ref"] != "#/components/responses/HealthResponse" {
		t.Fatalf("bundled response reference is missing: %#v", response)
	}
}

func TestBundleRejectsRemoteAndEscapingReferences(t *testing.T) {
	for name, reference := range map[string]string{
		"remote": "https://example.com/health.yaml",
		"escape": "../health.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, "openapi.yaml", rootFixture(reference))
			err := bundle(filepath.Join(root, "openapi.yaml"), filepath.Join(root, "bundle.json"))
			if err == nil {
				t.Fatal("bundle unexpectedly accepted a forbidden reference")
			}
		})
	}
}

func TestBundleRejectsUnreferencedDomainFiles(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "openapi.yaml", rootFixture("./domains/health.yaml"))
	writeFixture(t, root, "domains/health.yaml", healthFixture())
	writeFixture(t, root, "domains/orphan.yaml", healthFixture())

	err := bundle(filepath.Join(root, "openapi.yaml"), filepath.Join(root, "bundle.yaml"))
	if err == nil || !strings.Contains(err.Error(), "not reachable from root") {
		t.Fatalf("bundle did not reject the orphaned source: %v", err)
	}
}

func writeFixture(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rootFixture(reference string) string {
	return `openapi: 3.0.3
info:
  title: fixture
  version: 1.0.0
paths:
  /health:
    $ref: '` + reference + `#/paths/~1health'
components:
  responses:
    HealthResponse:
      $ref: '` + reference + `#/components/responses/HealthResponse'
`
}

func healthFixture() string {
	return `paths:
  /health:
    get:
      operationId: getHealth
      responses:
        '200':
          $ref: '#/components/responses/HealthResponse'
components:
  responses:
    HealthResponse:
      description: healthy
`
}
