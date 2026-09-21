package emulationstationimport

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

func TestRuntimeCheckRejectsMalformedDependencySnapshot(t *testing.T) {
	t.Parallel()
	for _, snapshot := range []string{
		`{"dependencies":7}`, `{"dependencies":[{"requiredEntries":7}]}`, `{"bios":7}`,
		`{"multiDisc":{"missingEntries":[{"ordinal":"two"}]}}`,
	} {
		t.Run(snapshot, func(t *testing.T) {
			t.Parallel()
			value, err := projectRuntimeCheck(sql.NullString{String: "BLOCKED", Valid: true}, sql.NullString{String: "LAUNCH_PARENT_MISSING", Valid: true}, sql.NullString{}, sql.NullString{}, sql.NullString{String: snapshot, Valid: true})
			var cause *json.UnmarshalTypeError
			if value != nil || !errors.As(err, &cause) {
				t.Fatalf("corrupt runtime snapshot returned value=%#v error=%v", value, err)
			}
		})
	}
}

func TestRuntimeCheckPreservesEmptyDiagnosticArrays(t *testing.T) {
	t.Parallel()
	for _, snapshot := range []string{"", "{}", `{"dependencies":[{"kind":"PARENT"}]}`} {
		t.Run(snapshot, func(t *testing.T) {
			t.Parallel()
			value, err := projectRuntimeCheck(sql.NullString{String: "READY", Valid: true}, sql.NullString{String: "READY", Valid: true}, sql.NullString{}, sql.NullString{}, sql.NullString{String: snapshot, Valid: true})
			if err != nil {
				t.Fatal(err)
			}
			arrays := []bool{value.MissingEntries != nil, value.MismatchedEntries != nil, value.Dependencies != nil, value.BIOS != nil, value.MissingDiscs != nil}
			for _, present := range arrays {
				if !present {
					t.Fatalf("missing diagnostic arrays: %#v", value)
				}
			}
			for _, dependency := range value.Dependencies {
				if dependency.RequiredEntries == nil {
					t.Fatal("missing requiredEntries array")
				}
			}
		})
	}
}
