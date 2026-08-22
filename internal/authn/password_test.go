package authn

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"retrom/internal/testassert"
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return password != "secure phrase 123" }), "password = %q, %v", password, err)
	_, err = ValidatePassword("COMMON PASSWORD 123", "COMMON PASSWORD 123", "alice", "Alice", testBlocklist{"common password 123": true})
	var policy *PasswordError
	testassert.Falsef(t, testassert.Any(func() bool { return !errors.As(err, &policy) }, func() bool { return policy.Reason != ReasonCommon }), "common password error = %v", err)
	_, err = ValidatePassword("different password", "different passwörd", "alice", "Alice", testBlocklist{})
	testassert.Falsef(t, testassert.Any(func() bool { return !errors.As(err, &policy) }, func() bool { return policy.Reason != ReasonConfirmation }), "confirmation error = %v", err)
}

func TestPasswordPolicyRequiresAtLeastSixUnicodeCharacters(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name      string
		candidate string
		valid     bool
	}{
		{name: "five ASCII characters", candidate: "A1!x2"},
		{name: "six ASCII characters", candidate: "A1!x2z", valid: true},
		{name: "five Unicode characters", candidate: "甲乙丙丁戊"},
		{name: "six Unicode characters", candidate: "甲乙丙丁戊己", valid: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			normalized, err := NormalizePassword(testCase.candidate)
			if testCase.valid {
				testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return normalized != testCase.candidate }), "NormalizePassword(%q) = %q, %v", testCase.candidate, normalized, err)
				return
			}
			var policy *PasswordError
			testassert.Falsef(t, testassert.Any(func() bool { return !errors.As(err, &policy) }, func() bool { return policy.Reason != ReasonTooShort }), "NormalizePassword(%q) error = %v", testCase.candidate, err)
		})
	}
}

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
		if _, err := hasher.Verify(context.Background(), "secure phrase 123", corrupted); !errors.Is(err, ErrCredential) {
			t.Fatalf("Verify(%q) error = %v", corrupted, err)
		}
	}
}
