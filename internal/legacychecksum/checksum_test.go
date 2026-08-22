package legacychecksum

import (
	"encoding/hex"
	"testing"

	"retrom/internal/testassert"
)

func TestCatalogDigestsAreAvailableWithoutSecurityIdentityAmbiguity(t *testing.T) {
	t.Parallel()
	value := []byte("retrom legacy catalog fixture")
	hashes := New()
	if _, err := hashes.MD5.Write(value); err != nil {
		t.Fatal(err)
	}
	if _, err := hashes.SHA1.Write(value); err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(hashes.MD5.Sum(nil)); got != "e383af002d292f299e1f40abe454464f" {
		t.Fatalf("MD5 = %s", got)
	}
	if got := hex.EncodeToString(hashes.SHA1.Sum(nil)); got != "1336da9f81688c3f025b02afbe4e21f7f2455c0f" {
		t.Fatalf("SHA-1 = %s", got)
	}
	md5Value, sha1Value := Sum(value)
	testassert.Falsef(t, testassert.Any(func() bool { return md5Value != "e383af002d292f299e1f40abe454464f" }, func() bool { return sha1Value != "1336da9f81688c3f025b02afbe4e21f7f2455c0f" }), "Sum() = %s/%s", md5Value, sha1Value)
}
