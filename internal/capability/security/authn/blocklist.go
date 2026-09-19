package authn

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	BlocklistSize   = 73_017
	blocklistLines  = 10_000
	blocklistSHA256 = "4adb3f0afb4a10cf19ebe48d8c69a46f934bbc8d77c694c210564f9583e7f4ba"
)

var ErrBlocklistInvalid = errors.New("PASSWORD_BLOCKLIST_INVALID")

// Blocklist is an immutable set of normalized password facts. Its zero value is empty.
type Blocklist struct{ values map[string]struct{} }

// DecodeBlocklist accepts only the pinned public password list.
func DecodeBlocklist(contents []byte) (*Blocklist, error) {
	digest := sha256.Sum256(contents)
	if len(contents) != BlocklistSize || hex.EncodeToString(digest[:]) != blocklistSHA256 || !utf8.Valid(contents) {
		return nil, ErrBlocklistInvalid
	}
	return decodeBlocklistLines(contents)
}

func decodeBlocklistLines(contents []byte) (*Blocklist, error) {
	values := make(map[string]struct{}, blocklistLines)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	scanner.Buffer(make([]byte, 1024), 4096)
	lines := 0
	for scanner.Scan() {
		line := scanner.Text()
		values[cases.Fold().String(norm.NFC.String(line))] = struct{}{}
		lines++
	}
	if scanner.Err() != nil || lines != blocklistLines {
		return nil, ErrBlocklistInvalid
	}
	return &Blocklist{values: values}, nil
}

// Contains queries a password that has already been normalized and folded.
func (blocklist *Blocklist) Contains(value string) bool {
	if blocklist == nil {
		return false
	}
	_, ok := blocklist.values[value]
	return ok
}
