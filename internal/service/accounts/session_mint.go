package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	model "retrom/internal/model/accounts"

	"github.com/google/uuid"
)

func MintSession(random io.Reader) (model.SessionMaterial, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.SessionMaterial{}, fmt.Errorf("generate session identity: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(random, raw); err != nil {
		return model.SessionMaterial{}, fmt.Errorf("generate session token: %w", err)
	}
	return model.SessionMaterial{
		ID: id.String(),
		Token: base64.RawURLEncoding.EncodeToString(
			raw,
		),
		Hash: sha256.Sum256(
			raw,
		),
	}, nil
}
