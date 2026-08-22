package cursor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const signatureDomain = "retrom-cursor-v1\x00"

var ErrInvalid = errors.New("INVALID_CURSOR")

type Payload struct {
	Version      int      `json:"version"`
	OperationID  string   `json:"operationId"`
	FilterDigest string   `json:"filterDigest"`
	SortCode     string   `json:"sortCode"`
	SortValues   []string `json:"sortValues"`
	ID           string   `json:"id"`
	ExpiresAtMS  int64    `json:"expiresAtMs"`
}

type Codec struct {
	key [32]byte
	now func() time.Time
}

func New(key [32]byte, now func() time.Time) *Codec { return &Codec{key: key, now: now} }

func FilterDigest(filter any) string {
	encoded, _ := json.Marshal(filter)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (codec *Codec) Encode(payload Payload) (string, error) {
	payload.Version = 1
	if payload.ExpiresAtMS == 0 {
		payload.ExpiresAtMS = codec.now().Add(24 * time.Hour).UnixMilli()
	}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) > 4096 {
		return "", ErrInvalid
	}
	mac := hmac.New(sha256.New, codec.key[:])
	_, _ = mac.Write([]byte(signatureDomain))
	_, _ = mac.Write(encoded)
	return base64.RawURLEncoding.EncodeToString(encoded) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (codec *Codec) Decode(token, operationID, filterDigest, sortCode string) (Payload, error) {
	encoded, signature, err := decodeToken(token)
	if err != nil || !codec.validSignature(encoded, signature) {
		return Payload{}, ErrInvalid
	}
	payload, err := decodePayload(encoded)
	if err != nil || !codec.validPayload(payload, operationID, filterDigest, sortCode) {
		return Payload{}, ErrInvalid
	}
	return payload, nil
}

func decodeToken(token string) ([]byte, []byte, error) {
	if len(token) == 0 || len(token) > 8192 {
		return nil, nil, ErrInvalid
	}
	payloadPart, signaturePart, found := strings.Cut(token, ".")
	if !found || strings.Contains(signaturePart, ".") {
		return nil, nil, ErrInvalid
	}
	encoded, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil || len(encoded) > 4096 {
		return nil, nil, ErrInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil || len(signature) != sha256.Size {
		return nil, nil, ErrInvalid
	}
	return encoded, signature, nil
}

func (codec *Codec) validSignature(encoded, signature []byte) bool {
	mac := hmac.New(sha256.New, codec.key[:])
	_, _ = mac.Write([]byte(signatureDomain))
	_, _ = mac.Write(encoded)
	return subtle.ConstantTimeCompare(signature, mac.Sum(nil)) == 1
}

func decodePayload(encoded []byte) (Payload, error) {
	var payload Payload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return Payload{}, ErrInvalid
	}
	return payload, nil
}

func (codec *Codec) validPayload(payload Payload, operationID, filterDigest, sortCode string) bool {
	return payload.Version == 1 &&
		payload.OperationID == operationID &&
		payload.FilterDigest == filterDigest &&
		payload.SortCode == sortCode &&
		payload.ExpiresAtMS > codec.now().UnixMilli() &&
		payload.ID != ""
}
