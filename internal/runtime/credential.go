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
	launchDomain       = "retrom-launch-v1\x00"
	cursorKeyDomain    = "retrom-cursor-key-v1"
	setupCodeDomain    = "retrom-setup-v1"
	accountKeyDomain   = "retrom-account-link-key-v1"
	accountLinkDomain  = "retrom-account-link-v1\x00"
	rateLimitKeyDomain = "retrom-rate-limit-key-v1"
	serverImportDomain = "server-import-root-key-v1"
)

var ErrLaunchKeyInvalid = errors.New("LAUNCH_KEY_INVALID")

type Credentials struct {
	key [32]byte
}

func LoadCredentials(dataDir string) (*Credentials, error) {
	key, err := readKey(filepath.Join(dataDir, "secrets", "launch-capability.key"))
	if err != nil {
		return nil, err
	}
	return &Credentials{key: key}, nil
}

func (credentials *Credentials) SetupCode() string {
	return EncodeCapability(credentials.derive(setupCodeDomain))
}

func (credentials *Credentials) MatchesSetupCode(value string) bool {
	expected := credentials.SetupCode()
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}

func (credentials *Credentials) AccountLinkToken(kind string, linkID uuid.UUID) string {
	key := credentials.derive(accountKeyDomain)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(accountLinkDomain))
	_, _ = mac.Write([]byte(kind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(linkID[:])
	result := make([]byte, 0, 48)
	result = append(result, linkID[:]...)
	result = append(result, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(result)
}

func (credentials *Credentials) ParseAccountLinkToken(kind, encoded string) (uuid.UUID, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != 48 || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return uuid.Nil, false
	}
	linkID, err := uuid.FromBytes(decoded[:16])
	if err != nil || linkID.Version() != 7 {
		return uuid.Nil, false
	}
	expected := credentials.AccountLinkToken(kind, linkID)
	return linkID, subtle.ConstantTimeCompare([]byte(encoded), []byte(expected)) == 1
}

func (credentials *Credentials) RateLimitSubject(scope, subject string) [32]byte {
	key := credentials.derive(rateLimitKeyDomain)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(scope))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(subject))
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func (credentials *Credentials) derive(domain string) [32]byte {
	mac := hmac.New(sha256.New, credentials.key[:])
	_, _ = mac.Write([]byte(domain))
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func LoadOrCreateCredentials(dataDir string) (*Credentials, error) {
	directory := filepath.Join(dataDir, "secrets")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create launch key directory: %w", err)
	}
	// Secret directories need owner execute permission and remain owner-only.
	if err := os.Chmod(directory, 0o700); err != nil {
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
	file, err := os.OpenFile(
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

// ServerImportRootDigest binds a configured ID to a canonical host path while
// keeping both the launch key and host path out of persisted task input.
func (credentials *Credentials) ServerImportRootDigest(rootID, canonicalPath string) [32]byte {
	key := credentials.derive(serverImportDomain)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(rootID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(canonicalPath))
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
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
