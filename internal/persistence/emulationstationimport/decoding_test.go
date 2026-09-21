package emulationstationimport

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestDecodeArrayPreservesEmptyValuesAndRejectsCorruption(t *testing.T) {
	t.Parallel()
	for _, stored := range []string{"[]", "null"} {
		var values []string
		if err := decodeArray(stored, &values); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(values, []string{}) {
			t.Fatalf("empty stored array=%#v", values)
		}
	}
	var values []string
	err := decodeArray("[7]", &values)
	var typeError *json.UnmarshalTypeError
	if !errors.As(err, &typeError) {
		t.Fatalf("lost array type failure: %v", err)
	}
	err = decodeArray("[", &values)
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) {
		t.Fatalf("lost array syntax failure: %v", err)
	}
}
