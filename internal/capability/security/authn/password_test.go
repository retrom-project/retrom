package authn

import (
	"errors"
	"testing"

	"retrom/internal/testkit/testassert"
)

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
	password, err := ValidatePassword("secure phrase 123", "secure phrase 123", "alice", "Alice", &Blocklist{})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return password != "secure phrase 123" }), "password = %q, %v", password, err)
	_, err = ValidatePassword("COMMON PASSWORD 123", "COMMON PASSWORD 123", "alice", "Alice", &Blocklist{values: map[string]struct{}{"common password 123": {}}})
	var policy *PasswordError
	testassert.Falsef(t, testassert.Any(func() bool { return !errors.As(err, &policy) }, func() bool { return policy.Reason != ReasonCommon }), "common password error = %v", err)
	_, err = ValidatePassword("different password", "different passwörd", "alice", "Alice", &Blocklist{})
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
