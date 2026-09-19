package authn

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"

	authnpolicy "retrom/internal/capability/security/authn"
	accountsmodel "retrom/internal/model/accounts"
)

var _ accountsmodel.PasswordHasher = (*PasswordHasher)(nil)

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
	salt := make([]byte, authnpolicy.ArgonSaltBytes)
	if _, err := io.ReadFull(hasher.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	if err := hasher.acquire(ctx); err != nil {
		return "", fmt.Errorf("acquire password hash worker: %w", err)
	}
	hash := argon2.IDKey(
		[]byte(normalized), salt, authnpolicy.ArgonIterations, authnpolicy.ArgonMemory,
		authnpolicy.ArgonParallelism, authnpolicy.ArgonHashBytes,
	)
	<-hasher.semaphore
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return authnpolicy.EncodePHC(salt, hash), nil
}

func (hasher *PasswordHasher) Verify(ctx context.Context, normalized, encoded string) (bool, error) {
	salt, expected, err := authnpolicy.ParsePHC(encoded)
	if err != nil {
		// The closed PHC parser has one rejection identity. Keep the same
		// sentinel as the original Verify, without introducing a wrapper.
		return false, authnpolicy.ErrCredential
	}
	if err := hasher.acquire(ctx); err != nil {
		return false, fmt.Errorf("acquire password verification worker: %w", err)
	}
	actual := argon2.IDKey(
		[]byte(normalized), salt, authnpolicy.ArgonIterations, authnpolicy.ArgonMemory,
		authnpolicy.ArgonParallelism, authnpolicy.ArgonHashBytes,
	)
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
