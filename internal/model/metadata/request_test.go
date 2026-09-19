package metadata

import (
	"errors"
	"strings"
	"testing"
)

func TestLookupRequestPreservesCanonicalHashIdentity(t *testing.T) {
	t.Parallel()
	hashes := ContentHashes{CRC32: "1234abcd", MD5: strings.Repeat("b", 32), SHA1: strings.Repeat("c", 40), SHA256: strings.Repeat("a", 64)}
	body, canonical, err := LookupRequest(hashes)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"crc":"1234abcd","mD5":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","shA1":"cccccccccccccccccccccccccccccccccccccccc","shA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}` {
		t.Fatalf("request bytes = %s", body)
	}
	if string(canonical) != `{"body":{"crc":"1234abcd","mD5":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","shA1":"cccccccccccccccccccccccccccccccccccccccc","shA256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"endpointContract":"BY_HASH_V1","provider":"HASHEOUS"}` {
		t.Fatalf("canonical identity = %s", canonical)
	}
	digest, err := RequestDigest(hashes)
	if err != nil || digest != "e7113781b95642dfeb7119708a28899abe49cb0a57d6ab7f176d09cb5d5f38ef" {
		t.Fatalf("request digest = %s / %v", digest, err)
	}
}

func TestLookupRequestRejectsInvalidHashFacts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		hashes ContentHashes
		want   error
	}{
		{"missing", ContentHashes{}, errMissingContentHash},
		{"uppercase", ContentHashes{CRC32: "1234ABCD"}, errInvalidContentHash},
		{"wrong length", ContentHashes{SHA1: "abc"}, errInvalidContentHash},
		{"not hexadecimal", ContentHashes{MD5: strings.Repeat("g", 32)}, errInvalidContentHash},
		{"partial invalid", ContentHashes{CRC32: "1234abcd", SHA256: "bad"}, errInvalidContentHash},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, canonical, err := LookupRequest(test.hashes)
			if !errors.Is(err, test.want) || body != nil || canonical != nil {
				t.Fatalf("invalid hashes accepted: %s / %s / %v", body, canonical, err)
			}
			digest, err := RequestDigest(test.hashes)
			if !errors.Is(err, test.want) || digest != "" {
				t.Fatalf("invalid digest accepted: %s / %v", digest, err)
			}
		})
	}
}
