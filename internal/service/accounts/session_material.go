package accounts

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"time"

	"retrom/internal/authn"
)

const (
	idleDuration     = 8 * time.Hour
	absoluteDuration = 24 * time.Hour
	refreshInterval  = 5 * time.Minute
)

type SessionMaterial struct {
	ID, Token string
	Hash      [32]byte
}

func (snapshot SessionSnapshot) view(token string, raw []byte) Session {
	principal := authn.Principal{
		UserID:         snapshot.User.UserID,
		ProfileID:      snapshot.ProfileID,
		Username:       snapshot.User.Username,
		DisplayName:    snapshot.User.DisplayName,
		Role:           snapshot.User.Role,
		SessionID:      snapshot.ID,
		SessionVersion: snapshot.SessionVersion,
		SessionToken:   token,
	}
	return Session{
		Principal: principal,
		User:      snapshot.User,
		CSRFToken: csrfToken(
			raw,
		),
		IdleExpiresAtMS:     snapshot.IdleExpiry,
		AbsoluteExpiresAtMS: snapshot.AbsoluteExpiry,
		CookieToken:         token,
	}
}

func (material SessionMaterial) View(user User, profileID string, version, now int64) Session {
	raw, _ := base64.RawURLEncoding.DecodeString(material.Token)
	return (SessionSnapshot{
		ID:             material.ID,
		User:           user,
		ProfileID:      profileID,
		SessionVersion: version,
		IdleExpiry:     now + idleDuration.Milliseconds(),
		AbsoluteExpiry: now + absoluteDuration.Milliseconds(),
	}).view(
		material.Token,
		raw,
	)
}

func (material SessionMaterial) Record(userID string, version, now int64) SessionRecord {
	return SessionRecord{
		ID:             material.ID,
		UserID:         userID,
		Hash:           material.Hash,
		SessionVersion: version,
		CreatedAt:      now,
		LastSeen:       now,
		IdleExpiry:     now + idleDuration.Milliseconds(),
		AbsoluteExpiry: now + absoluteDuration.Milliseconds(),
	}
}

func csrfToken(raw []byte) string {
	mac := hmac.New(sha256.New, raw)
	_, _ = mac.Write([]byte("retrom-csrf-v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func MatchesCSRF(sessionToken, supplied string) bool {
	raw, err := decodeToken(sessionToken)
	if err != nil {
		return false
	}
	expected := csrfToken(raw)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(supplied)) == 1
}

func decodeToken(encoded string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return nil, ErrAuthenticationNeeded
	}
	return raw, nil
}
