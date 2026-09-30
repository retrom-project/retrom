package jobinput

import (
	"bytes"
	"errors"
	"testing"
)

func TestRetryKeepsFrozenInputsAndCreatesNewExecutionIdentity(t *testing.T) {
	scope := Scope{Type: "IMPORT_ITEM", ID: "item"}
	encoded, err := Encode("attachment", scope, map[string]any{"baseSnapshotId": "snapshot", "limits": map[string]int{"maxBytes": 10}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := Decode(encoded, "attachment", scope)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := Retry(encoded, "attachment", scope)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Decode(retried, "attachment", scope)
	if err != nil {
		t.Fatal(err)
	}
	if before.ExecutionID == after.ExecutionID || !bytes.Equal(before.Inputs, after.Inputs) {
		t.Fatalf("input replaced: %s -> %s", encoded, retried)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schemaVersion":1,"baseSnapshotId":"snapshot"}`),
		bytes.ReplaceAll(encoded, []byte(`"kind":"attachment"`), []byte(`"kind":"other"`)),
		bytes.ReplaceAll(encoded, []byte(`"id":"item"`), []byte(`"id":"other"`)),
		bytes.ReplaceAll(encoded, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":2`)),
	} {
		if _, err := Retry(invalid, "attachment", scope); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted incompatible envelope=%s err=%v", invalid, err)
		}
	}
}
