package authn

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	blocklistRelativePath = "auth/password-blocklists/v1/payload/10k-most-common.txt"
	blocklistSize         = 73_017
	blocklistLines        = 10_000
	blocklistSHA256       = "4adb3f0afb4a10cf19ebe48d8c69a46f934bbc8d77c694c210564f9583e7f4ba"
)

var ErrBlocklistInvalid = errors.New("PASSWORD_BLOCKLIST_INVALID")

type FileBlocklist struct{ values map[string]struct{} }

func LoadBlocklist(dependencyRoot string) (*FileBlocklist, error) {
	path := filepath.Join(dependencyRoot, filepath.FromSlash(blocklistRelativePath))
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: run make prepare-deps", ErrBlocklistInvalid)
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	limited := io.LimitReader(file, blocklistSize+1)
	contents, err := io.ReadAll(io.TeeReader(limited, digest))
	if err != nil || len(contents) != blocklistSize || hex.EncodeToString(digest.Sum(nil)) != blocklistSHA256 ||
		!utf8.Valid(contents) {
		return nil, ErrBlocklistInvalid
	}
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
	return &FileBlocklist{values: values}, nil
}

func (blocklist *FileBlocklist) Contains(value string) bool {
	_, ok := blocklist.values[value]
	return ok
}
