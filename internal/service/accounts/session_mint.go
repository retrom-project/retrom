package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func MintSession(random io.Reader) (SessionMaterial, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return SessionMaterial{}, fmt.Errorf("generate session identity: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(random, raw); err != nil {
		return SessionMaterial{}, fmt.Errorf("generate session token: %w", err)
	}
	return SessionMaterial{
		ID: id.String(),
		Token: base64.RawURLEncoding.EncodeToString(
			raw,
		),
		Hash: sha256.Sum256(
			raw,
		),
	}, nil
}
