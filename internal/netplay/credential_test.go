package netplay

import (
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/testassert"

	"github.com/google/uuid"
)

func TestCredentialIsPurposeBoundAndStoredOwnerOnly(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	credentials, err := LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	info, err := os.Stat(filepath.Join(dataDir, "secrets", "netplay-capability.key"))
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return info.Mode().Perm() != 0o600 }, func() bool { return info.Size() != 32 }), "netplay key = %#v, %v", info, err)
	sessionID := uuid.MustParse("01980000-0000-7000-8000-000000000001")
	profileID := uuid.MustParse("01980000-0000-7000-8000-000000000002")
	first := credentials.Capability(sessionID, profileID, 1)
	second := credentials.Capability(sessionID, profileID, 2)
	firstHash := HashCapability(first)
	testassert.False(t, testassert.Any(func() bool { return first == second }, func() bool { return !MatchesCapability(EncodeCapability(first), firstHash[:]) }), "generation binding or capability verification failed")
}
