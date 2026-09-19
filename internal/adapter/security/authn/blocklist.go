package authn

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	authnpolicy "retrom/internal/capability/security/authn"
)

const blocklistRelativePath = "auth/password-blocklists/v1/payload/10k-most-common.txt"

func LoadBlocklist(dependencyRoot string) (*authnpolicy.Blocklist, error) {
	path := filepath.Join(dependencyRoot, filepath.FromSlash(blocklistRelativePath))
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: run make prepare-deps", authnpolicy.ErrBlocklistInvalid)
	}
	return readBlocklist(file)
}

func readBlocklist(file io.ReadCloser) (*authnpolicy.Blocklist, error) {
	defer func() { _ = file.Close() }()
	contents, err := io.ReadAll(io.LimitReader(file, authnpolicy.BlocklistSize+1))
	if err != nil {
		return nil, authnpolicy.ErrBlocklistInvalid
	}
	blocklist, err := authnpolicy.DecodeBlocklist(contents)
	if err != nil {
		return nil, authnpolicy.ErrBlocklistInvalid
	}
	return blocklist, nil
}
