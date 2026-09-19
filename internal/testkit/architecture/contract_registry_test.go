package architecture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationSchemaRejectsMissingGuardWriteAndDigestCoordination(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compileOperationSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	base := contractOperationFixture()
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOperationSchema(schema, encoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"fresh_facts", "guards", "write_set", "time_rule", "content_coordination"} {
		t.Run(field, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			delete(document, field)
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateOperationSchema(schema, data); err == nil {
				t.Fatal("omitted operation contract field passed")
			}
		})
	}
	base.ContentCoordination.Required = true
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOperationSchema(schema, data); err == nil {
		t.Fatal("content reference publisher without digest lock definitions passed")
	}
}

func contractOperationFixture() OperationRecord {
	return OperationRecord{
		ID: "fixture.rename", Owner: "fixture", RF: "RF08",
		Consumers: []string{"fixture.Rename"}, Snapshot: "fixture.Snapshot",
		Prepare: "fixture.Prepare", Commit: "fixture.Commit", Policies: []string{},
		Facts: []OperationFact{{Name: "owner", Reader: "fixture.Read", Reason: "current owner"}},
		Guards: []OperationGuard{{
			ID: "owner", Fields: []string{"profile_id"}, Expected: "authenticated profile",
			Rejection: "not found", Test: "RFA-RF08-save_authority",
		}},
		Writes: []OperationWrite{{
			Stage: "rename", Helper: "fixture.Update", Records: []string{"save_states.name"},
			Test: "RFA-RF08-save_atomic",
		}},
		Idempotency: "expected version", AfterCommit: []OperationEffect{}, TimeRule: "clock after write permission",
		FaultPoints: []string{"rename"}, Tests: []string{"RFA-RF08-save_atomic"},
		ContentCoordination: ContentCoordination{
			Reason: "no content reference creation", DigestSources: []string{}, Tests: []string{},
		},
	}
}

func TestOperationSchemaCannotFetchUndeclaredRemoteReferences(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "quality", "architecture", "operations.schema.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"$ref":"https://untrusted.invalid/schema"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := compileOperationSchema(root); err == nil || !strings.Contains(err.Error(), errExternalSchema.Error()) {
		t.Fatalf("external schema was not rejected: %v", err)
	}
}

func TestContractReferencesCannotResolveProductionSymbolsFromTests(t *testing.T) {
	t.Parallel()
	index := contractIndex{
		symbols: map[string][]GoSymbol{
			"fixture.Commit": {{Name: "fixture.Commit", File: "internal/repo/fixture/commit_test.go"}},
			"fixture.Read":   {{Name: "fixture.Read", File: "internal/repo/fixture/read.go"}},
		},
		tests: map[string]VerificationCase{},
	}
	violations := inspectRegisteredSymbols("operations.json", "fixture", []string{"fixture.Commit", "fixture.Read"}, index)
	if len(violations) != 1 || !strings.Contains(violations[0].Message, "fixture.Commit") {
		t.Fatalf("test-only implementation passed as a production definition: %v", violations)
	}
	if violations := inspectRegisteredTests("operations.json", "fixture", []string{"made-up-case"}, index); len(violations) != 1 {
		t.Fatalf("unregistered case passed: %v", violations)
	}
}

func TestContractNamedTestRequiresExactSourceAndGoSignature(t *testing.T) {
	t.Parallel()
	test := VerificationCase{
		ID: "RFA-RF01-source_scope", Required: true, Deadline: 10,
		File: "internal/testkit/architecture/refactor_rf01_unit_test.go", Symbol: "TestRefactorRF01_source_scope",
	}
	registry := VerificationRegistry{SchemaVersion: 1, Baseline: strings.Repeat("a", 40), Cases: []VerificationCase{test}}
	symbol := GoSymbol{
		Name: "fixture." + test.Symbol, File: test.File, Kind: "function", Type: "func(t *testing.T)",
	}
	index := contractIndex{symbols: map[string][]GoSymbol{symbol.Name: {symbol}}}
	if violations := inspectTestDefinitions(registry, index); len(violations) != 0 {
		t.Fatalf("valid test declaration rejected: %v", violations)
	}
	for _, invalid := range []GoSymbol{
		{Name: symbol.Name, File: "internal/fixture/wrong_test.go", Kind: "function", Type: symbol.Type},
		{Name: symbol.Name, File: symbol.File, Kind: "function", Type: "func()"},
		{Name: symbol.Name, File: symbol.File, Kind: "variable", Type: symbol.Type},
	} {
		index.symbols[symbol.Name] = []GoSymbol{invalid}
		if violations := inspectTestDefinitions(registry, index); len(violations) != 1 {
			t.Fatalf("wrong source or non-test declaration passed: %+v: %v", invalid, violations)
		}
	}
}
