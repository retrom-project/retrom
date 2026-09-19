//go:build integration

package authn

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	authnpolicy "retrom/internal/capability/security/authn"
)

type blocklistLoadingGolden struct {
	SourceRevision string
	PayloadSHA256  string
	PayloadSize    int
	Membership     []struct {
		Query    string
		Contains bool
	}
	Loads  []blocklistLoadCase
	Policy []blocklistPinnedPolicy
}

type blocklistLoadCase struct {
	Name               string
	Error              string
	IsBlocklistInvalid bool
	IsNotExist         bool
	NonNil             bool
}

type blocklistPinnedPolicy struct {
	Name              string
	PasswordHex       string
	ConfirmationHex   string
	Username          string
	DisplayName       string
	Normalized        string
	Error             string
	Reason            authnpolicy.PasswordReason
	IsPasswordInvalid bool
}

func readBlocklistGolden(t *testing.T) blocklistLoadingGolden {
	t.Helper()
	contents, err := os.ReadFile("testdata/blocklist-loading-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden blocklistLoadingGolden
	if err := json.Unmarshal(contents, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.SourceRevision != "43b73ec928f6d88bfe041657c9884a6cf0f52e3f" ||
		golden.PayloadSize != 73017 || golden.PayloadSHA256 != "4adb3f0afb4a10cf19ebe48d8c69a46f934bbc8d77c694c210564f9583e7f4ba" {
		t.Fatal("old Go public dependency characterization changed")
	}
	if len(golden.Loads) != 8 || len(golden.Membership) != 10 || len(golden.Policy) != 10 {
		t.Fatal("old Go loading characterization cases changed")
	}
	return golden
}

func readPublicBlocklistPayload(t *testing.T) []byte {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "data")
	payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(blocklistRelativePath)))
	if err != nil {
		t.Fatalf("read pinned public dependency; run make prepare-deps: %v", err)
	}
	return payload
}

func TestBlocklistLoadedFactsMatchOldGo(t *testing.T) {
	t.Parallel()
	golden := readBlocklistGolden(t)
	blocklist, err := LoadBlocklist(filepath.Join("..", "..", "..", "..", "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range golden.Membership {
		if actual := blocklist.Contains(sample.Query); actual != sample.Contains {
			t.Fatalf("old membership changed for %q: %t", sample.Query, actual)
		}
	}
	for _, sample := range golden.Policy {
		t.Run(sample.Name, func(t *testing.T) { assertPinnedBlocklistPolicy(t, blocklist, sample) })
	}
}

func assertPinnedBlocklistPolicy(t *testing.T, blocklist *authnpolicy.Blocklist, sample blocklistPinnedPolicy) {
	t.Helper()
	password, err := hex.DecodeString(sample.PasswordHex)
	if err != nil {
		t.Fatal(err)
	}
	confirmation, err := hex.DecodeString(sample.ConfirmationHex)
	if err != nil {
		t.Fatal(err)
	}
	result, err := authnpolicy.ValidatePassword(string(password), string(confirmation), sample.Username, sample.DisplayName, blocklist)
	message := ""
	if err != nil {
		message = err.Error()
	}
	if result != sample.Normalized || message != sample.Error ||
		errors.Is(err, authnpolicy.ErrPasswordInvalid) != sample.IsPasswordInvalid {
		t.Fatalf("loaded facts changed old password result: %q, %v", result, err)
	}
	reason := authnpolicy.PasswordReason("")
	var policy *authnpolicy.PasswordError
	if errors.As(err, &policy) {
		reason = policy.Reason
	}
	if reason != sample.Reason {
		t.Fatalf("loaded facts changed old password reason: %q", reason)
	}
}

func TestBlocklistLoadingMatchesOldGo(t *testing.T) {
	t.Parallel()
	payload := readPublicBlocklistPayload(t)
	for _, sample := range readBlocklistGolden(t).Loads {
		t.Run(sample.Name, func(t *testing.T) {
			root := prepareBlocklistLoadInput(t, sample.Name, payload)
			blocklist, err := LoadBlocklist(root)
			message := ""
			if err != nil {
				message = err.Error()
			}
			if message != sample.Error || (blocklist != nil) != sample.NonNil ||
				errors.Is(err, authnpolicy.ErrBlocklistInvalid) != sample.IsBlocklistInvalid ||
				errors.Is(err, os.ErrNotExist) != sample.IsNotExist {
				t.Fatalf("old load result changed: has facts=%t error=%v", blocklist != nil, err)
			}
		})
	}
}

func prepareBlocklistLoadInput(t *testing.T, name string, payload []byte) string {
	t.Helper()
	root := t.TempDir()
	if name == "missing" {
		return root
	}
	target := filepath.Join(root, filepath.FromSlash(blocklistRelativePath))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if name == "directory" {
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		return root
	}
	contents := bytes.Clone(payload)
	switch name {
	case "empty":
		contents = nil
	case "truncated":
		contents = contents[:len(contents)-1]
	case "oversized":
		contents = append(contents, 'x')
	case "same-size-changed":
		contents[0] ^= 1
	case "invalid-utf8":
		contents[0] = 0xff
	case "valid":
	default:
		t.Fatalf("unrecognized old Go load sample %q", name)
	}
	if err := os.WriteFile(target, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBlocklistCloseFailureRemainsNonFatal(t *testing.T) {
	t.Parallel()
	payload := readPublicBlocklistPayload(t)
	input := &blocklistInput{reader: bytes.NewReader(payload), closeErr: errors.New("public fixture close failure")}
	blocklist, err := readBlocklist(input)
	if err != nil || blocklist == nil || !blocklist.Contains("password") {
		t.Fatalf("close failure invalidated verified facts: %v", err)
	}
	if input.closeCalls != 1 || input.bytesRead != len(payload) {
		t.Fatalf("successful load changed read/close boundary: %d, %d", input.bytesRead, input.closeCalls)
	}
}

func TestBlocklistDecodeOwnsLoadedBytes(t *testing.T) {
	t.Parallel()
	payload := readPublicBlocklistPayload(t)
	blocklist, err := authnpolicy.DecodeBlocklist(payload)
	if err != nil {
		t.Fatal(err)
	}
	clear(payload)
	for _, sample := range readBlocklistGolden(t).Membership {
		if blocklist.Contains(sample.Query) != sample.Contains {
			t.Fatalf("caller mutation changed decoded fact %q", sample.Query)
		}
	}
}

func TestBlocklistReadErrorRejectsCompletePayload(t *testing.T) {
	t.Parallel()
	payload := readPublicBlocklistPayload(t)
	failure := errors.New("public fixture read failure after complete payload")
	input := &blocklistInput{reader: bytes.NewReader(payload), readErr: failure}
	blocklist, err := readBlocklist(input)
	if blocklist != nil || !errors.Is(err, authnpolicy.ErrBlocklistInvalid) || errors.Is(err, failure) {
		t.Fatalf("complete bytes hid the read failure: %v", err)
	}
	if input.bytesRead != len(payload) || input.closeCalls != 1 {
		t.Fatalf("failed complete read changed cleanup: %d, %d", input.bytesRead, input.closeCalls)
	}
}
