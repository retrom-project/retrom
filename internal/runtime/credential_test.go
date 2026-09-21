package runtime

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"retrom/internal/testassert"

	"github.com/google/uuid"
)

func TestCredentialsConcurrentCreationConverges(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	var first, second *Credentials
	var firstErr, secondErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); first, firstErr = LoadOrCreateCredentials(directory) }()
	go func() { defer wait.Done(); second, secondErr = LoadOrCreateCredentials(directory) }()
	wait.Wait()
	testassert.Falsef(t, testassert.Any(func() bool { return firstErr != nil }, func() bool { return secondErr != nil }), "LoadOrCreateCredentials() errors = %v, %v", firstErr, secondErr)
	launchID := uuid.MustParse("01980000-0000-7000-8000-000000000001")
	if left, right := first.Capability(launchID), second.Capability(launchID); !bytes.Equal(left[:], right[:]) {
		t.Fatal("concurrent initialization produced different keys")
	}
	info, err := os.Stat(filepath.Join(directory, "secrets", "launch-capability.key"))
	testassert.Falsef(t, err != nil, "stat launch key: %v", err)
	testassert.Falsef(t, testassert.Any(func() bool { return info.Mode().Perm() != 0o600 }, func() bool { return info.Size() != 32 }), "launch key mode/size = %o/%d", info.Mode().Perm(), info.Size())
}

func TestCredentialsRejectSymlink(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	secretDir := filepath.Join(directory, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(secretDir, "launch-capability.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateCredentials(directory); err == nil {
		t.Fatal("symlink launch key was accepted")
	}
}

func TestCredentialPurposeSeparationAndAccountLinkValidation(t *testing.T) {
	t.Parallel()
	credentials, err := LoadOrCreateCredentials(t.TempDir())
	testassert.False(t, err != nil, err)
	if code := credentials.SetupCode(); len(code) != 43 || !credentials.MatchesSetupCode(code) || credentials.MatchesSetupCode(code+"x") {
		t.Fatalf("setup code validation failed: length=%d", len(code))
	}
	linkID := uuid.MustParse("01980000-0000-7000-8000-000000000001")
	token := credentials.AccountLinkToken("INVITATION", linkID)
	if parsed, ok := credentials.ParseAccountLinkToken("INVITATION", token); !ok || parsed != linkID {
		t.Fatalf("invitation token = %s, %v", parsed, ok)
	}
	if _, ok := credentials.ParseAccountLinkToken("PASSWORD_RESET", token); ok {
		t.Fatal("invitation token accepted as password reset")
	}
	login := credentials.RateLimitSubject("LOGIN_ACCOUNT", "alice")
	setup := credentials.RateLimitSubject("SETUP_IP", "alice")
	testassert.False(t, bytes.Equal(login[:], setup[:]), "rate-limit scopes were not separated")
	root := credentials.ServerImportRootDigest("bios-root", "/srv/bios")
	same := credentials.ServerImportRootDigest("bios-root", "/srv/bios")
	changedID := credentials.ServerImportRootDigest("other-root", "/srv/bios")
	changedPath := credentials.ServerImportRootDigest("bios-root", "/srv/other")
	testassert.False(t, testassert.Any(func() bool { return !bytes.Equal(root[:], same[:]) }, func() bool { return bytes.Equal(root[:], changedID[:]) }, func() bool { return bytes.Equal(root[:], changedPath[:]) }, func() bool { return bytes.Equal(root[:], login[:]) }), "server import root digest is not deterministic and purpose separated")
}
