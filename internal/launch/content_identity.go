package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

const RuntimeContentPath = "/runtime/content/"

func ContentIdentity(content ContentView) (string, error) {
	if !validContentDigest(content.Digest) {
		return "", ErrBlocked
	}
	if content.Format == "" || content.CoreID == "" || content.ProviderID == "" || content.TargetID == "" ||
		!validContentDigest(content.TargetContractSHA256) ||
		content.Format == "RETROM_DOS_DIRECT_ZIP_V1" && content.CoreID != "dosbox_pure" {
		return "", ErrBlocked
	}
	digestInput := "RETROM_RUNTIME_GAME_V2\x00" + content.Format + "\x00" + content.ProviderID + "\x00" +
		content.TargetID + "\x00" + content.TargetContractSHA256 + "\x00" + content.Digest + "\x00" +
		nullableDOSEntry(content.DOSEntry)
	digest := sha256.Sum256([]byte(digestInput))
	return hex.EncodeToString(digest[:]), nil
}

func ExternalContentIdentity(digest string) (string, error) {
	if !validContentDigest(digest) {
		return "", ErrBlocked
	}
	derived := sha256.Sum256([]byte("RETROM_RUNTIME_EXTERNAL_V1\x00" + digest))
	return hex.EncodeToString(derived[:]), nil
}

func BundleIdentity(files []BundleFile) (string, error) {
	if len(files) == 0 {
		return "", ErrBlocked
	}
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(left, right BundleFile) int {
		return strings.Compare(left.LogicalName, right.LogicalName)
	})
	digest := sha256.New()
	_, _ = digest.Write([]byte("RETROM_LAUNCH_BUNDLE_V1\x00"))
	previousName := ""
	for _, file := range ordered {
		if !validRuntimeLogicalName(file.LogicalName) ||
			!validContentDigest(file.SHA256) {
			return "", ErrBlocked
		}
		if previousName == file.LogicalName {
			return "", ErrBlocked
		}
		_, _ = fmt.Fprintf(digest, "%d\x00%s\x00%s\x00", len(file.LogicalName), file.LogicalName, file.SHA256)
		previousName = file.LogicalName
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func RuntimeContentURL(kind, identity, logicalName string) (string, error) {
	if !validRuntimeContentKind(kind) || !validContentDigest(identity) || !validRuntimeLogicalName(logicalName) {
		return "", ErrBlocked
	}
	return RuntimeContentPath + kind + "/" + identity + "/" + url.PathEscape(logicalName), nil
}

func validRuntimeLogicalName(value string) bool {
	if value == "" || len(value) > 255 || !utf8.ValidString(value) || path.Base(value) != value ||
		strings.Contains(value, "\\") {
		return false
	}
	for _, character := range value {
		if character == 0 || character >= 1 && character <= 31 || character == 127 {
			return false
		}
	}
	return true
}

func validRuntimeContentKind(kind string) bool {
	return kind == "game" || kind == "external" || kind == "bios" || kind == "parent"
}

func validContentDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		isDigit := character >= '0' && character <= '9'
		isLowerHex := character >= 'a' && character <= 'f'
		if !isDigit && !isLowerHex {
			return false
		}
	}
	return true
}

func nullableDOSEntry(entry *string) string {
	if entry == nil {
		return "<menu>"
	}
	return *entry
}
