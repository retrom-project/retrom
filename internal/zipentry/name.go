package zipentry

import (
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

var ErrInvalidName = errors.New("ZIP_ENTRY_NAME_INVALID")

// DecodeName mirrors the legacy ZIP filename rule used by import scanning.
// Valid UTF-8 is authoritative. An explicitly non-UTF-8 name is decoded as
// GB18030 so runtime views can address the same normalized member recorded at
// import time.
func DecodeName(value string, nonUTF8 bool) (string, error) {
	if utf8.ValidString(value) {
		return value, nil
	}
	if !nonUTF8 {
		return "", ErrInvalidName
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().String(value)
	if err != nil || !utf8.ValidString(decoded) || strings.ContainsRune(decoded, utf8.RuneError) {
		return "", ErrInvalidName
	}
	return decoded, nil
}
