package filestore

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

var ErrRecordInvalid = errors.New("invalid domain file record")

// Record is a file value stored with its business record. There is no global
// file registry: a record contains everything needed to read the file.
type Record struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	MD5       string `json:"md5"`
	SHA1      string `json:"sha1"`
	CRC32     string `json:"crc32"`
	Size      int64  `json:"size_bytes"`
	MediaType string `json:"media_type"`
}

func ParseRecord(value string) (Record, error) {
	var record Record
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("decode domain file record: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Record{}, ErrRecordInvalid
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (record Record) Validate() error {
	if !RemovablePath(record.Path) || record.Size < 0 || record.MediaType == "" {
		return ErrRecordInvalid
	}
	for _, digest := range []struct {
		value string
		bytes int
	}{{record.SHA256, 32}, {record.MD5, 16}, {record.SHA1, 20}, {record.CRC32, 4}} {
		decoded, err := hex.DecodeString(digest.value)
		if err != nil || len(decoded) != digest.bytes || strings.ToLower(digest.value) != digest.value {
			return ErrRecordInvalid
		}
	}
	return nil
}

func (record Record) Encode() (string, error) {
	if err := record.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("encode domain file record: %w", err)
	}
	return string(encoded), nil
}

func safeRelativePath(value string) bool {
	return value != "." && fs.ValidPath(value) && !strings.ContainsAny(value, "\\\x00:")
}

// FileRecord validates prepared facts without writing any database state.
func FileRecord(metadata Metadata, mediaType string) (string, error) {
	record, err := ParseRecord(metadata.Record)
	if err != nil {
		return "", err
	}
	if record.SHA256 != metadata.SHA256 || record.MD5 != metadata.MD5 || record.SHA1 != metadata.SHA1 ||
		record.CRC32 != metadata.CRC32 || record.Size != metadata.Size {
		return "", ErrRecordInvalid
	}
	if mediaType == "" {
		return "", ErrRecordInvalid
	}
	return metadata.Record, nil
}
