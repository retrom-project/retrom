package authn

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type testBlocklist map[string]bool

func (values testBlocklist) Contains(value string) bool { return values[value] }

func TestIdentityNormalization(t *testing.T) {
	t.Parallel()
	if username, err := NormalizeUsername(" alice "); err != nil || username != "alice" {
		t.Fatalf("username = %q, %v", username, err)
	}
	for _, value := range []string{"Alice", "ab", "local", "a space", "éclair"} {
		if _, err := NormalizeUsername(value); !errors.Is(err, ErrUsernameInvalid) {
			t.Fatalf("NormalizeUsername(%q) error = %v", value, err)
		}
	}
	if display, err := NormalizeDisplayName("  A\u030Alice　"); err != nil || display != "Ålice" {
		t.Fatalf("display = %q, %v", display, err)
	}
}

func TestPasswordPolicyUsesNFCFoldAndExactBlocklist(t *testing.T) {
	t.Parallel()
	password, err := ValidatePassword("secure phrase 123", "secure phrase 123", "alice", "Alice", testBlocklist{})
	if err != nil || password != "secure phrase 123" {
		t.Fatalf("password = %q, %v", password, err)
	}
	_, err = ValidatePassword("COMMON PASSWORD 123", "COMMON PASSWORD 123", "alice", "Alice", testBlocklist{"common password 123": true})
	var policy *PasswordError
	if !errors.As(err, &policy) || policy.Reason != ReasonCommon {
		t.Fatalf("common password error = %v", err)
	}
	_, err = ValidatePassword("different password", "different passwörd", "alice", "Alice", testBlocklist{})
	if !errors.As(err, &policy) || policy.Reason != ReasonConfirmation {
		t.Fatalf("confirmation error = %v", err)
	}
}

func TestArgon2IDV1RoundTripAndStrictParser(t *testing.T) {
	t.Parallel()
	hasher := newPasswordHasher(bytes.NewReader(bytes.Repeat([]byte{7}, 32)), 1)
	encoded, err := hasher.Hash(context.Background(), "secure phrase 123")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := hasher.Verify(context.Background(), "secure phrase 123", encoded)
	if err != nil || !ok {
		t.Fatalf("verify = %t, %v", ok, err)
	}
	ok, err = hasher.Verify(context.Background(), "secure phrase 124", encoded)
	if err != nil || ok {
		t.Fatalf("wrong verify = %t, %v", ok, err)
	}
	for _, corrupted := range []string{
		"$argon2id$v=19$m=65536,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		encoded + "=",
	} {
		if _, err := hasher.Verify(context.Background(), "secure phrase 123", corrupted); !errors.Is(err, ErrCredential) {
			t.Fatalf("Verify(%q) error = %v", corrupted, err)
		}
	}
}
