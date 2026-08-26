package checkpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"retrom/internal/importing"
)

func validateManifest(manifest Manifest, payloadSize int64) error {
	if manifest.SchemaVersion != 1 || !validEngine(manifest.Engine) || !validResumeSlot(manifest) ||
		len(manifest.Entries) > MaxEntries || payloadSize < 0 {
		return ErrInvalid
	}
	var expectedOffset int64
	var previousStore Store
	var previousKey string
	for index, entry := range manifest.Entries {
		if !validManifestEntry(entry, expectedOffset, payloadSize) {
			return ErrInvalid
		}
		if index > 0 && !manifestEntryAfter(entry, previousStore, previousKey) {
			return ErrInvalid
		}
		expectedOffset += entry.SizeBytes
		previousStore, previousKey = entry.Store, entry.Key
	}
	if expectedOffset != payloadSize {
		return ErrInvalid
	}
	return nil
}

func validManifestEntry(entry ManifestEntry, expectedOffset, payloadSize int64) bool {
	return validStore(entry.Store) && validKey(entry) && entry.MediaType == "application/octet-stream" &&
		entry.Offset == expectedOffset && entry.SizeBytes >= 0 && validSHA256(entry.SHA256) &&
		entry.SizeBytes <= payloadSize-entry.Offset
}

func manifestEntryAfter(entry ManifestEntry, previousStore Store, previousKey string) bool {
	return entry.Store > previousStore || entry.Store == previousStore && entry.Key > previousKey
}

func validResumeSlot(manifest Manifest) bool {
	if manifest.ResumeSlot < 1 || manifest.ResumeSlot > 2147483647 {
		return false
	}
	if manifest.Engine == EngineRPG2000 || manifest.Engine == EngineRPG2003 {
		return manifest.ResumeSlot == 100
	}
	return true
}

func validKey(entry ManifestEntry) bool {
	if !utf8.ValidString(entry.Key) || len(entry.Key) > 1024 || strings.ContainsRune(entry.Key, 0) {
		return false
	}
	if entry.Store == StoreFilesystem {
		_, err := importing.ValidateLogicalPath(entry.Key)
		return err == nil
	}
	return norm.NFC.IsNormalString(entry.Key)
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validEngine(value Engine) bool {
	switch value {
	case EngineRPG2000, EngineRPG2003, EngineRPGMV, EngineRPGMZ:
		return true
	default:
		return false
	}
}

func validStore(value Store) bool {
	switch value {
	case StoreFilesystem, StoreLocalStorage, StoreLocalForage, StoreRetromNative:
		return true
	default:
		return false
	}
}
