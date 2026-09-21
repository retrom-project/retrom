package runtimebundle

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestSharedLaunchEnvelopeFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "api", "runtime-provider", "v1", "fixtures")
	for _, test := range []struct {
		directory string
		valid     bool
	}{{directory: "valid", valid: true}, {directory: "invalid", valid: false}} {
		entries, err := os.ReadDir(filepath.Join(root, test.directory))
		if err != nil {
			t.Fatal(err)
		}
		sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
		for _, entry := range entries {
			t.Run(test.directory+"/"+entry.Name(), func(t *testing.T) {
				contents, err := os.ReadFile(filepath.Join(root, test.directory, entry.Name()))
				if err != nil {
					t.Fatal(err)
				}
				_, err = ParseLaunchEnvelope(contents)
				if test.valid && err != nil {
					t.Fatalf("valid fixture rejected: %v", err)
				}
				if !test.valid && !errors.Is(err, ErrLaunchEnvelopeInvalid) {
					t.Fatalf("invalid fixture accepted or returned wrong error: %v", err)
				}
			})
		}
	}
}

func TestJSONSchemaAgreesWithSharedLaunchEnvelopeFixtures(t *testing.T) {
	root := filepath.Join("..", "..", "api", "runtime-provider", "v1")
	schema := compileLaunchEnvelopeSchema(t, root)
	fixtureRoot := filepath.Join(root, "fixtures")
	for _, test := range []struct {
		directory string
		valid     bool
	}{{directory: "valid", valid: true}, {directory: "invalid", valid: false}} {
		assertSchemaFixtureDirectory(t, schema, fixtureRoot, test.directory, test.valid)
	}
}

func compileLaunchEnvelopeSchema(t *testing.T, root string) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.UseRegexpEngine(compileECMAScriptRegexp)
	for _, name := range []string{
		"common.schema.json", "provider-manifest.schema.json", "runtime-resource.schema.json", "launch-envelope.schema.json",
	} {
		contents, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contents))
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if err := compiler.AddResource("https://retrom.dev/schema/runtime-provider/v1/"+name, document); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	schema, err := compiler.Compile("https://retrom.dev/schema/runtime-provider/v1/launch-envelope.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func assertSchemaFixtureDirectory(
	t *testing.T,
	schema *jsonschema.Schema,
	fixtureRoot, directory string,
	valid bool,
) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixtureRoot, directory))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Run(directory+"/"+entry.Name(), func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join(fixtureRoot, directory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			instance, validationErr := parseStrictJSON(contents)
			if validationErr == nil {
				validationErr = schema.Validate(instance)
			}
			if valid && validationErr != nil {
				t.Fatalf("valid fixture rejected: %v", validationErr)
			}
			if !valid && validationErr == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}

type ecmaScriptRegexp regexp2.Regexp

func compileECMAScriptRegexp(source string) (jsonschema.Regexp, error) {
	compiled, err := regexp2.Compile(source, regexp2.ECMAScript)
	return (*ecmaScriptRegexp)(compiled), err
}

func (expression *ecmaScriptRegexp) MatchString(value string) bool {
	matched, err := (*regexp2.Regexp)(expression).MatchString(value)
	return err == nil && matched
}

func (expression *ecmaScriptRegexp) String() string {
	return (*regexp2.Regexp)(expression).String()
}
