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
