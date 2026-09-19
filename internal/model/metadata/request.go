package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

var (
	errInvalidContentHash = errors.New("invalid content hash")
	errMissingContentHash = errors.New("at least one content hash is required")
)

func RequestDigest(hashes ContentHashes) (string, error) {
	_, canonical, err := LookupRequest(hashes)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// LookupRequest encodes the stable provider lookup body and cache identity.
func LookupRequest(hashes ContentHashes) ([]byte, []byte, error) {
	fields := []struct {
		key    string
		value  string
		length int
	}{{"crc", hashes.CRC32, 8}, {"mD5", hashes.MD5, 32}, {"shA1", hashes.SHA1, 40}, {"shA256", hashes.SHA256, 64}}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		if field.value != strings.ToLower(field.value) || len(field.value) != field.length {
			return nil, nil, errInvalidContentHash
		}
		if _, err := hex.DecodeString(field.value); err != nil {
			return nil, nil, errInvalidContentHash
		}
		parts = append(parts, strconv.Quote(field.key)+":"+strconv.Quote(field.value))
	}
	if len(parts) == 0 {
		return nil, nil, errMissingContentHash
	}
	body := []byte("{" + strings.Join(parts, ",") + "}")
	canonical := []byte(`{"body":` + string(body) + `,"endpointContract":"BY_HASH_V1","provider":"HASHEOUS"}`)
	return body, canonical, nil
}
