package netplay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestCredentialIsPurposeBoundAndStoredOwnerOnly(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	credentials, err := LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dataDir, "secrets", "netplay-capability.key"))
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() != 32 {
		t.Fatalf("netplay key = %#v, %v", info, err)
	}
	sessionID := uuid.MustParse("01980000-0000-7000-8000-000000000001")
	profileID := uuid.MustParse("01980000-0000-7000-8000-000000000002")
	first := credentials.Capability(sessionID, profileID, 1)
	second := credentials.Capability(sessionID, profileID, 2)
	firstHash := HashCapability(first)
	if first == second || !MatchesCapability(EncodeCapability(first), firstHash[:]) {
		t.Fatal("generation binding or capability verification failed")
	}
}
