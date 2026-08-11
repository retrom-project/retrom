package runtime

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

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
	if firstErr != nil || secondErr != nil {
		t.Fatalf("LoadOrCreateCredentials() errors = %v, %v", firstErr, secondErr)
	}
	launchID := uuid.MustParse("01980000-0000-7000-8000-000000000001")
	if left, right := first.Capability(launchID), second.Capability(launchID); !bytes.Equal(left[:], right[:]) {
		t.Fatal("concurrent initialization produced different keys")
	}
	info, err := os.Stat(filepath.Join(directory, "secrets", "launch-capability.key"))
	if err != nil {
		t.Fatalf("stat launch key: %v", err)
	}
	if info.Mode().Perm() != 0o600 || info.Size() != 32 {
		t.Fatalf("launch key mode/size = %o/%d", info.Mode().Perm(), info.Size())
	}
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
	if err != nil {
		t.Fatal(err)
	}
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
	if bytes.Equal(login[:], setup[:]) {
		t.Fatal("rate-limit scopes were not separated")
	}
	root := credentials.ServerImportRootDigest("bios-root", "/srv/bios")
	same := credentials.ServerImportRootDigest("bios-root", "/srv/bios")
	changedID := credentials.ServerImportRootDigest("other-root", "/srv/bios")
	changedPath := credentials.ServerImportRootDigest("bios-root", "/srv/other")
	if !bytes.Equal(root[:], same[:]) || bytes.Equal(root[:], changedID[:]) || bytes.Equal(root[:], changedPath[:]) ||
		bytes.Equal(root[:], login[:]) {
		t.Fatal("server import root digest is not deterministic and purpose separated")
	}
}
