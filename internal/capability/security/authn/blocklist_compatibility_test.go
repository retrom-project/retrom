package authn

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

type blocklistPolicyCase struct {
	Name              string
	PasswordHex       string
	ConfirmationHex   string
	Username          string
	DisplayName       string
	Blocklist         string
	Entries           map[string]bool
	Normalized        string
	Error             string
	Reason            PasswordReason
	IsPasswordInvalid bool
}

func TestBlocklistPolicyMatchesOldGo(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/blocklist-policy-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		SourceRevision string
		Policy         []blocklistPolicyCase
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.SourceRevision != "43b73ec928f6d88bfe041657c9884a6cf0f52e3f" || len(golden.Policy) != 14 {
		t.Fatal("old Go policy characterization changed")
	}
	for _, sample := range golden.Policy {
		t.Run(sample.Name, func(t *testing.T) { assertBlocklistPolicySample(t, sample) })
	}
}

func assertBlocklistPolicySample(t *testing.T, sample blocklistPolicyCase) {
	t.Helper()
	password, err := hex.DecodeString(sample.PasswordHex)
	if err != nil {
		t.Fatal(err)
	}
	confirmation, err := hex.DecodeString(sample.ConfirmationHex)
	if err != nil {
		t.Fatal(err)
	}
	var blocklist *Blocklist
	switch sample.Blocklist {
	case "nil":
	case "empty":
		blocklist = &Blocklist{}
	case "custom":
		blocklist = &Blocklist{values: make(map[string]struct{}, len(sample.Entries))}
		for value, member := range sample.Entries {
			if member {
				blocklist.values[value] = struct{}{}
			}
		}
	default:
		t.Fatalf("unexpected pure fixture source %q", sample.Blocklist)
	}
	normalized, err := ValidatePassword(string(password), string(confirmation), sample.Username, sample.DisplayName, blocklist)
	message := ""
	if err != nil {
		message = err.Error()
	}
	if normalized != sample.Normalized || message != sample.Error || errors.Is(err, ErrPasswordInvalid) != sample.IsPasswordInvalid {
		t.Fatalf("old policy result changed: normalized=%q error=%v", normalized, err)
	}
	var policy *PasswordError
	reason := PasswordReason("")
	if errors.As(err, &policy) {
		reason = policy.Reason
	}
	if reason != sample.Reason {
		t.Fatalf("old policy reason changed: %q", reason)
	}
}
