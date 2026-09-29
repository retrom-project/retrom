package runtime

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"github.com/google/uuid"
)

var errRuntimeSessionID = errors.New("invalid runtime session id")

func (credentials *Credentials) RuntimeSession(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.String() != id {
		return "", errRuntimeSessionID
	}
	mac := hmac.New(sha256.New, credentials.key[:])
	_, _ = mac.Write([]byte("retrom-runtime-session-v1\x00"))
	_, _ = mac.Write(parsed[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
