package checkpoint

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
)

func Encode(engine Engine, resumeSlot int64, entries []Entry) ([]byte, error) {
	if len(entries) > MaxEntries {
		return nil, ErrInvalid
	}
	ordered := cloneAndSort(entries)
	manifest := Manifest{SchemaVersion: 1, Engine: engine, ResumeSlot: resumeSlot}
	manifest.Entries = make([]ManifestEntry, 0, len(ordered))
	var payloadSize int64
	for _, entry := range ordered {
		digest := sha256.Sum256(entry.Data)
		manifest.Entries = append(manifest.Entries, ManifestEntry{
			Store: entry.Store, Key: entry.Key, MediaType: entry.MediaType,
			Offset: payloadSize, SizeBytes: int64(len(entry.Data)), SHA256: hex.EncodeToString(digest[:]),
		})
		payloadSize += int64(len(entry.Data))
	}
	if err := validateManifest(manifest, payloadSize); err != nil {
		return nil, err
	}
	manifestBytes := marshalCanonical(manifest)
	if len(manifestBytes) > MaxManifestBytes {
		return nil, ErrInvalid
	}
	totalSize := int64(len(Magic)+4+len(manifestBytes)) + payloadSize
	if totalSize > MaxBundleBytes {
		return nil, ErrInvalid
	}
	result := make([]byte, 0, int(totalSize))
	result = append(result, Magic...)
	var manifestLength [4]byte
	binary.BigEndian.PutUint32(manifestLength[:], uint32(len(manifestBytes)))
	result = append(result, manifestLength[:]...)
	result = append(result, manifestBytes...)
	for _, entry := range ordered {
		result = append(result, entry.Data...)
	}
	return result, nil
}

func Decode(contents []byte) (Bundle, error) {
	if len(contents) < len(Magic)+4 || len(contents) > MaxBundleBytes || string(contents[:len(Magic)]) != Magic {
		return Bundle{}, ErrInvalid
	}
	manifestSize := int(binary.BigEndian.Uint32(contents[len(Magic) : len(Magic)+4]))
	manifestStart := len(Magic) + 4
	manifestEnd := manifestStart + manifestSize
	if manifestSize == 0 || manifestSize > MaxManifestBytes || manifestEnd > len(contents) {
		return Bundle{}, ErrInvalid
	}
	manifest, err := unmarshalCanonical(contents[manifestStart:manifestEnd])
	if err != nil {
		return Bundle{}, err
	}
	payload := contents[manifestEnd:]
	if err := validateManifest(manifest, int64(len(payload))); err != nil {
		return Bundle{}, err
	}
	entries := make([]Entry, 0, len(manifest.Entries))
	for _, item := range manifest.Entries {
		start, end := int(item.Offset), int(item.Offset+item.SizeBytes)
		data := payload[start:end]
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != item.SHA256 {
			return Bundle{}, ErrInvalid
		}
		entries = append(entries, Entry{
			Store: item.Store, Key: item.Key, MediaType: item.MediaType,
			Data: append([]byte(nil), data...),
		})
	}
	return Bundle{Manifest: manifest, Entries: entries}, nil
}

func DecodeExpected(contents []byte, expected Expected) (Bundle, error) {
	bundle, err := Decode(contents)
	if err != nil {
		return Bundle{}, err
	}
	if bundle.Manifest.Engine != expected.Engine || bundle.Manifest.ResumeSlot != expected.ResumeSlot {
		return Bundle{}, fmt.Errorf("%w: expected engine or resume slot mismatch", ErrInvalid)
	}
	return bundle, nil
}

func cloneAndSort(entries []Entry) []Entry {
	result := make([]Entry, len(entries))
	for index, entry := range entries {
		result[index] = entry
		result[index].Data = append([]byte(nil), entry.Data...)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Store != result[right].Store {
			return result[left].Store < result[right].Store
		}
		return result[left].Key < result[right].Key
	})
	return result
}
