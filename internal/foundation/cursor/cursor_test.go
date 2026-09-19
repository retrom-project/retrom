package cursor

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

type legacyCapture struct {
	SourceHead string                `json:"sourceHead"`
	KeyHex     string                `json:"keyHex"`
	Filters    []legacyFilterCapture `json:"filters"`
	Tokens     []legacyTokenCapture  `json:"tokens"`
}

type legacyFilterCapture struct {
	Name    string `json:"name"`
	Encoded string `json:"encoded"`
	Digest  string `json:"digest"`
}

type legacyTokenCapture struct {
	Name        string  `json:"name"`
	NowMS       int64   `json:"nowMs"`
	Input       Payload `json:"input"`
	PayloadJSON string  `json:"payloadJson"`
	Token       string  `json:"token"`
}

func TestCodecMatchesLegacyV1Golden(t *testing.T) {
	t.Parallel()
	capture := loadLegacyCapture(t)
	codec := New(legacyKey(t, capture.KeyHex))
	for _, filter := range capture.Filters {
		t.Run("filter/"+filter.Name, func(t *testing.T) {
			if actual := FilterDigest([]byte(filter.Encoded)); actual != filter.Digest {
				t.Fatalf("digest = %q, want %q", actual, filter.Digest)
			}
		})
	}
	for _, captured := range capture.Tokens {
		t.Run("token/"+captured.Name, func(t *testing.T) {
			token, err := codec.Encode(captured.Input, captured.NowMS)
			if err != nil {
				t.Fatal(err)
			}
			if token != captured.Token {
				t.Fatalf("token = %q, want %q", token, captured.Token)
			}
			assertPayloadBytes(t, token, captured.PayloadJSON)
			decoded, err := codec.Decode(
				captured.Token,
				captured.Input.OperationID,
				captured.Input.FilterDigest,
				captured.Input.SortCode,
				captured.NowMS,
			)
			if err != nil {
				t.Fatal(err)
			}
			var expected Payload
			if err := json.Unmarshal([]byte(captured.PayloadJSON), &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, expected) {
				t.Fatalf("decoded = %#v, want %#v", decoded, expected)
			}
		})
	}
}

func TestCodecBindsOperationFilterSortAndStrictExpiry(t *testing.T) {
	t.Parallel()
	nowMS := int64(1_786_000_000_000)
	codec := New([32]byte{1, 2, 3})
	filter := FilterDigest([]byte(`{"q":"mario"}`))
	token, err := codec.Encode(
		Payload{
			OperationID:  "getGames",
			FilterDigest: filter,
			SortCode:     "TITLE_ASC",
			SortValues:   []string{"Mario"},
			ID:           "01980000-0000-7000-8000-000000000001",
		},
		nowMS,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(token, "getGames", filter, "TITLE_ASC", nowMS); err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]string{
		"operation": "getSaves",
		"filter":    strings.Repeat("0", 64),
		"sort":      "UPDATED_DESC",
	} {
		t.Run(name, func(t *testing.T) {
			operation, digest, sortCode := "getGames", filter, "TITLE_ASC"
			switch name {
			case "operation":
				operation = candidate
			case "filter":
				digest = candidate
			case "sort":
				sortCode = candidate
			}
			if _, err := codec.Decode(token, operation, digest, sortCode, nowMS); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	const expiryMS = int64(1_786_086_400_000)
	if _, err := codec.Decode(token, "getGames", filter, "TITLE_ASC", expiryMS-1); err != nil {
		t.Fatalf("before expiry: %v", err)
	}
	if _, err := codec.Decode(token, "getGames", filter, "TITLE_ASC", expiryMS); !errors.Is(err, ErrInvalid) {
		t.Fatalf("at expiry error = %v", err)
	}
}

func TestCodecRejectsInvalidDefaultExpiryCalculation(t *testing.T) {
	t.Parallel()
	codec := New([32]byte{1})
	payload := Payload{
		OperationID: "getGames", FilterDigest: strings.Repeat("0", 64),
		SortCode: "TITLE_ASC", SortValues: []string{"Fixture"}, ID: "fixture",
	}
	for name, nowMS := range map[string]int64{
		"negative": -1,
		"overflow": math.MaxInt64 - defaultTTLMS + 1,
	} {
		t.Run(name, func(t *testing.T) {
			if token, err := codec.Encode(payload, nowMS); token != "" || !errors.Is(err, ErrInvalid) {
				t.Fatalf("token = %q, error = %v", token, err)
			}
		})
	}
	token, err := codec.Encode(payload, math.MaxInt64-defaultTTLMS)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(
		token, payload.OperationID, payload.FilterDigest, payload.SortCode, math.MaxInt64-defaultTTLMS,
	)
	if err != nil || decoded.ExpiresAtMS != math.MaxInt64 {
		t.Fatalf("decoded = %#v, error = %v", decoded, err)
	}

	payload.ExpiresAtMS = 1
	for name, nowMS := range map[string]int64{
		"negative-now": -1,
		"maximum-now":  math.MaxInt64,
	} {
		t.Run("explicit-expiry/"+name, func(t *testing.T) {
			if _, err := codec.Encode(payload, nowMS); err != nil {
				t.Fatalf("explicit expiry was recomputed: %v", err)
			}
		})
	}
}

func loadLegacyCapture(t *testing.T) legacyCapture {
	t.Helper()
	contents, err := os.ReadFile("testdata/legacy_v1_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var result legacyCapture
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatal(err)
	}
	if result.SourceHead != "f39243478403695fbd95e68ac83da59f9e021c5b" {
		t.Fatalf("source head = %q", result.SourceHead)
	}
	return result
}

func legacyKey(t *testing.T, value string) [32]byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("key = %q, error = %v", value, err)
	}
	var result [32]byte
	copy(result[:], decoded)
	return result
}

func assertPayloadBytes(t *testing.T, token, expected string) {
	t.Helper()
	payloadPart, _, found := strings.Cut(token, ".")
	if !found {
		t.Fatal("token omitted signature separator")
	}
	encoded, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != expected {
		t.Fatalf("payload bytes = %q, want %q", encoded, expected)
	}
}
