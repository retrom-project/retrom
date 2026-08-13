package netplay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

const capabilityMACDomain = "retrom-netplay-v1\x00"

var ErrCredentialKeyInvalid = errors.New("NETPLAY_KEY_INVALID")

type Credentials struct {
	key [32]byte
}

func LoadOrCreateCredentials(dataDir string) (*Credentials, error) {
	directory := filepath.Join(dataDir, "secrets")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create netplay key directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil { //nolint:gosec // Owner-only secret directory.
		return nil, fmt.Errorf("secure netplay key directory: %w", err)
	}
	target := filepath.Join(directory, "netplay-capability.key")
	key, err := readCredentialKey(target)
	if err == nil {
		return &Credentials{key: key}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var candidate [32]byte
	if _, err := io.ReadFull(rand.Reader, candidate[:]); err != nil {
		return nil, fmt.Errorf("generate netplay key: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".netplay-key-")
	if err != nil {
		return nil, fmt.Errorf("create netplay key candidate: %w", err)
	}
	temporaryName := temporary.Name()
	defer cleanup.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("secure netplay key candidate: %w", err)
	}
	if _, err := temporary.Write(candidate[:]); err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("write netplay key candidate: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("sync netplay key candidate: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("close netplay key candidate: %w", err)
	}
	if err := os.Link(temporaryName, target); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("publish netplay key: %w", err)
	}
	if err := syncCredentialDirectory(directory); err != nil {
		return nil, err
	}
	key, err = readCredentialKey(target)
	if err != nil {
		return nil, err
	}
	return &Credentials{key: key}, nil
}

func readCredentialKey(path string) ([32]byte, error) {
	var key [32]byte
	info, err := os.Lstat(path)
	if err != nil {
		return key, fmt.Errorf("netplay/credential: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || info.Size() != 32 {
		return key, ErrCredentialKeyInvalid
	}
	file, err := os.OpenFile( //nolint:gosec // Fixed secret slot with O_NOFOLLOW.
		path,
		os.O_RDONLY|credentialNoFollow(),
		0,
	)
	if err != nil {
		return key, fmt.Errorf("open netplay key: %w", err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	if _, err := io.ReadFull(file, key[:]); err != nil {
		return key, ErrCredentialKeyInvalid
	}
	var extra [1]byte
	if count, err := file.Read(extra[:]); err != io.EOF || count != 0 {
		return key, ErrCredentialKeyInvalid
	}
	return key, nil
}

func syncCredentialDirectory(path string) error {
	directory, err := os.Open(path) //nolint:gosec // Fixed secrets directory below the configured data root.
	if err != nil {
		return fmt.Errorf("open netplay key directory for sync: %w", err)
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync netplay key directory: %w", err)
	}
	return nil
}

func (credentials *Credentials) Capability(sessionID, profileID uuid.UUID, generation uint32) [32]byte {
	mac := hmac.New(sha256.New, credentials.key[:])
	_, _ = mac.Write([]byte(capabilityMACDomain))
	_, _ = mac.Write(sessionID[:])
	_, _ = mac.Write(profileID[:])
	var encodedGeneration [4]byte
	binary.BigEndian.PutUint32(encodedGeneration[:], generation)
	_, _ = mac.Write(encodedGeneration[:])
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func EncodeCapability(value [32]byte) string {
	return base64.RawURLEncoding.EncodeToString(value[:])
}

func HashCapability(value [32]byte) [32]byte {
	return sha256.Sum256(value[:])
}

func MatchesCapability(encoded string, expected []byte) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	canonical := base64.RawURLEncoding.EncodeToString(decoded)
	if err != nil || len(decoded) != 32 || len(expected) != 32 || canonical != encoded {
		return false
	}
	digest := sha256.Sum256(decoded)
	return subtle.ConstantTimeCompare(digest[:], expected) == 1
}
