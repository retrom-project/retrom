package authn

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	argonMemory      = 19_456
	argonIterations  = 2
	argonParallelism = 1
	argonSaltBytes   = 16
	argonHashBytes   = 32
)

var (
	ErrUsernameInvalid = errors.New("USERNAME_INVALID")
	ErrDisplayInvalid  = errors.New("DISPLAY_NAME_INVALID")
	ErrPasswordInvalid = errors.New("PASSWORD_POLICY_VIOLATION")
	ErrCredential      = errors.New("AUTH_CREDENTIAL_STORE_INVALID")
)

type PasswordReason string

const (
	ReasonTooShort     PasswordReason = "TOO_SHORT"
	ReasonTooLong      PasswordReason = "TOO_LONG"
	ReasonControl      PasswordReason = "CONTROL_CHARACTER"
	ReasonCommon       PasswordReason = "COMMON_PASSWORD"
	ReasonContext      PasswordReason = "CONTEXT_PASSWORD"
	ReasonConfirmation PasswordReason = "CONFIRMATION_MISMATCH"
)

type PasswordError struct{ Reason PasswordReason }

func (failure *PasswordError) Error() string { return string(failure.Reason) }
func (failure *PasswordError) Unwrap() error { return ErrPasswordInvalid }

func NormalizeUsername(value string) (string, error) {
	value = strings.Trim(value, " \t\r\n\v\f")
	if len(value) < 3 || len(value) > 32 || value[0] < 'a' || value[0] > 'z' {
		return "", ErrUsernameInvalid
	}
	for _, character := range value {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				if character != '.' && character != '_' && character != '-' {
					return "", ErrUsernameInvalid
				}
			}
		}
	}
	switch value {
	case "local", "root", "system", "retrom":
		return "", ErrUsernameInvalid
	}
	return value, nil
}

func NormalizeDisplayName(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrDisplayInvalid
	}
	value = strings.TrimFunc(value, unicode.IsSpace)
	value = norm.NFC.String(value)
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", ErrDisplayInvalid
		}
		count++
	}
	if count < 1 || count > 80 || len(value) > 320 {
		return "", ErrDisplayInvalid
	}
	return value, nil
}

type Blocklist interface {
	Contains(foldedPassword string) bool
}

type EmptyBlocklist struct{}

func (EmptyBlocklist) Contains(string) bool { return false }

func NormalizePassword(password string) (string, error) {
	if !utf8.ValidString(password) {
		return "", &PasswordError{Reason: ReasonControl}
	}
	password = norm.NFC.String(password)
	count := 0
	for _, character := range password {
		if unicode.IsControl(character) {
			return "", &PasswordError{Reason: ReasonControl}
		}
		count++
	}
	if count < 15 {
		return "", &PasswordError{Reason: ReasonTooShort}
	}
	if count > 128 || len(password) > 512 {
		return "", &PasswordError{Reason: ReasonTooLong}
	}
	return password, nil
}

func NormalizeLoginPassword(password string) (string, error) {
	if !utf8.ValidString(password) || len(password) == 0 || len(password) > 512 {
		return "", ErrPasswordInvalid
	}
	password = norm.NFC.String(password)
	if utf8.RuneCountInString(password) > 128 {
		return "", ErrPasswordInvalid
	}
	for _, character := range password {
		if unicode.IsControl(character) {
			return "", ErrPasswordInvalid
		}
	}
	return password, nil
}

func ValidatePassword(password, confirmation, username, displayName string, blocklist Blocklist) (string, error) {
	normalized, err := NormalizePassword(password)
	if err != nil {
		return "", err
	}
	normalizedConfirmation := norm.NFC.String(confirmation)
	if subtle.ConstantTimeCompare([]byte(normalized), []byte(normalizedConfirmation)) != 1 {
		return "", &PasswordError{Reason: ReasonConfirmation}
	}
	folded := cases.Fold().String(normalized)
	if folded == cases.Fold().String(username) || folded == cases.Fold().String(displayName) || folded == "retrom" {
		return "", &PasswordError{Reason: ReasonContext}
	}
	if blocklist != nil && blocklist.Contains(folded) {
		return "", &PasswordError{Reason: ReasonCommon}
	}
	return normalized, nil
}

type PasswordHasher struct {
	random    io.Reader
	semaphore chan struct{}
}

func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{random: rand.Reader, semaphore: make(chan struct{}, 4)}
}

func newPasswordHasher(random io.Reader, parallel int) *PasswordHasher {
	return &PasswordHasher{random: random, semaphore: make(chan struct{}, parallel)}
}

func (hasher *PasswordHasher) Hash(ctx context.Context, normalized string) (string, error) {
	salt := make([]byte, argonSaltBytes)
	if _, err := io.ReadFull(hasher.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	if err := hasher.acquire(ctx); err != nil {
		return "", fmt.Errorf("acquire password hash worker: %w", err)
	}
	hash := argon2.IDKey([]byte(normalized), salt, argonIterations, argonMemory, argonParallelism, argonHashBytes)
	<-hasher.semaphore
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return encodePHC(salt, hash), nil
}

func (hasher *PasswordHasher) Verify(ctx context.Context, normalized, encoded string) (bool, error) {
	salt, expected, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	if err := hasher.acquire(ctx); err != nil {
		return false, fmt.Errorf("acquire password verification worker: %w", err)
	}
	actual := argon2.IDKey([]byte(normalized), salt, argonIterations, argonMemory, argonParallelism, argonHashBytes)
	<-hasher.semaphore
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("verify password: %w", err)
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func (hasher *PasswordHasher) acquire(ctx context.Context) error {
	select {
	case hasher.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for password worker: %w", ctx.Err())
	}
}

func encodePHC(salt, hash []byte) string {
	return "$argon2id$v=19$m=19456,t=2,p=1$" +
		base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash)
}

func parsePHC(encoded string) ([]byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" ||
		parts[3] != "m=19456,t=2,p=1" {
		return nil, nil, ErrCredential
	}
	for _, value := range []string{"19456", "2", "1"} {
		if _, err := strconv.ParseUint(value, 10, 32); err != nil {
			return nil, nil, ErrCredential
		}
	}
	salt, saltErr := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	hash, hashErr := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if saltErr != nil || hashErr != nil || len(salt) != argonSaltBytes || len(hash) != argonHashBytes ||
		base64.RawStdEncoding.EncodeToString(salt) != parts[4] || base64.RawStdEncoding.EncodeToString(hash) != parts[5] {
		return nil, nil, ErrCredential
	}
	return salt, hash, nil
}

func ValidatePHC(encoded string) error {
	_, _, err := parsePHC(encoded)
	return err
}
