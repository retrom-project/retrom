package authn

import (
	"bytes"
	"context"
	"errors"
	"testing"

	authnpolicy "retrom/internal/capability/security/authn"
	"retrom/internal/testkit/testassert"
)

func TestArgon2IDV1RoundTripAndStrictParser(t *testing.T) {
	t.Parallel()
	hasher := newPasswordHasher(bytes.NewReader(bytes.Repeat([]byte{7}, 32)), 1)
	encoded, err := hasher.Hash(context.Background(), "secure phrase 123")
	testassert.False(t, err != nil, err)
	ok, err := hasher.Verify(context.Background(), "secure phrase 123", encoded)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !ok }), "verify = %t, %v", ok, err)
	ok, err = hasher.Verify(context.Background(), "secure phrase 124", encoded)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return ok }), "wrong verify = %t, %v", ok, err)
	for _, corrupted := range []string{
		"$argon2id$v=19$m=65536,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		encoded + "=",
	} {
		if _, err := hasher.Verify(context.Background(), "secure phrase 123", corrupted); !errors.Is(err, authnpolicy.ErrCredential) {
			t.Fatalf("Verify(%q) error = %v", corrupted, err)
		}
	}
}
