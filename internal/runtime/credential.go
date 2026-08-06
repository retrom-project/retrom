package runtime

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"retrom/internal/cleanup"

	"github.com/google/uuid"
)

const (
	launchDomain    = "retrom-launch-v1\x00"
	cursorKeyDomain = "retrom-cursor-key-v1"
)

var ErrLaunchKeyInvalid = errors.New("LAUNCH_KEY_INVALID")

type Credentials struct {
	key [32]byte
}

func LoadOrCreateCredentials(dataDir string) (*Credentials, error) {
	directory := filepath.Join(dataDir, "secrets")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create launch key directory: %w", err)
	}
	// Secret directories need owner execute permission and remain owner-only.
	if err := os.Chmod(directory, 0o700); err != nil { //nolint:gosec // Owner-only secret directory.
		return nil, fmt.Errorf("secure launch key directory: %w", err)
	}
	target := filepath.Join(directory, "launch-capability.key")
	key, err := readKey(target)
	if err == nil {
		return &Credentials{key: key}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	var candidate [32]byte
	if _, err := io.ReadFull(rand.Reader, candidate[:]); err != nil {
		return nil, fmt.Errorf("generate launch key: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".launch-key-")
	if err != nil {
		return nil, fmt.Errorf("create launch key candidate: %w", err)
	}
	temporaryName := temporary.Name()
	defer cleanup.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("secure launch key candidate: %w", err)
	}
	if _, err := temporary.Write(candidate[:]); err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("write launch key candidate: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup.Error("close", temporary.Close())
		return nil, fmt.Errorf("sync launch key candidate: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("close launch key candidate: %w", err)
	}
	if err := os.Link(temporaryName, target); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("publish launch key: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return nil, err
	}
	key, err = readKey(target)
	if err != nil {
		return nil, err
	}
	return &Credentials{key: key}, nil
}

func readKey(path string) ([32]byte, error) {
	var key [32]byte
	info, err := os.Lstat(path)
	if err != nil {
		return key, fmt.Errorf("runtime/credential: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || info.Size() != 32 {
		return key, ErrLaunchKeyInvalid
	}
	file, err := os.OpenFile( //nolint:gosec // Fixed secret slot with O_NOFOLLOW.
		path,
		os.O_RDONLY|syscallNoFollow(),
		0,
	)
	if err != nil {
		return key, fmt.Errorf("open launch key: %w", err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	if _, err := io.ReadFull(file, key[:]); err != nil {
		return key, ErrLaunchKeyInvalid
	}
	var extra [1]byte
	if count, err := file.Read(extra[:]); err != io.EOF || count != 0 {
		return key, ErrLaunchKeyInvalid
	}
	return key, nil
}

func (credentials *Credentials) Capability(launchID uuid.UUID) [32]byte {
	mac := hmac.New(sha256.New, credentials.key[:])
	_, _ = mac.Write([]byte(launchDomain))
	_, _ = mac.Write(launchID[:])
	var capability [32]byte
	copy(capability[:], mac.Sum(nil))
	return capability
}

// CursorKey derives a purpose-separated signing key without exposing the launch key.
func (credentials *Credentials) CursorKey() [32]byte {
	mac := hmac.New(sha256.New, credentials.key[:])
	_, _ = mac.Write([]byte(cursorKeyDomain))
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func EncodeCapability(capability [32]byte) string {
	return base64.RawURLEncoding.EncodeToString(capability[:])
}

func HashCapability(capability [32]byte) [32]byte {
	return sha256.Sum256(capability[:])
}

func MatchesCapability(encoded string, expectedHash []byte) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != 32 || len(expectedHash) != 32 {
		return false
	}
	digest := sha256.Sum256(decoded)
	return subtle.ConstantTimeCompare(digest[:], expectedHash) == 1
}

func syncDirectory(path string) error {
	directory, err := os.Open(path) //nolint:gosec // Fixed secrets directory under the validated data root.
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
