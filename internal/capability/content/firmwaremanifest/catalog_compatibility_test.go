package firmwaremanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestParseCatalogPreservesOldGoValuesAndErrors(t *testing.T) {
	contents, err := os.ReadFile("testdata/catalog-old-go-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	if hex.EncodeToString(sum[:]) != "9ab44e274456e99fcaa0797986eb9831fba78f05a71cb55079984cd54ec4f8b6" {
		t.Fatal("old-Go firmware characterization changed")
	}
	var cases []struct {
		Name, Input, JSON, Error, ErrorType string
		Invalid                             bool
	}
	if err := json.Unmarshal(contents, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 21 {
		t.Fatalf("cases = %d", len(cases))
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			value, parseErr := Parse([]byte(test.Input))
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			message, kind := "", ""
			if parseErr != nil {
				message, kind = parseErr.Error(), fmt.Sprintf("%T", parseErr)
			}
			if string(encoded) != test.JSON || message != test.Error ||
				kind != test.ErrorType || errors.Is(parseErr, ErrInvalid) != test.Invalid {
				t.Fatalf("compatibility mismatch: json=%s error=%q kind=%s invalid=%v", encoded, message, kind, errors.Is(parseErr, ErrInvalid))
			}
		})
	}
}
