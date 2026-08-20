package saves

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

var fileTreeBundleMagic = [8]byte{'R', 'E', 'T', 'P', 'S', 'P', '0', '1'}

const (
	fileTreeMaximumEntries   = 4_096
	fileTreeMaximumPathBytes = 1_024
)

func validFileTreePath(value []byte) bool {
	if len(value) == 0 || len(value) > fileTreeMaximumPathBytes || !utf8.Valid(value) ||
		value[0] == '/' || bytes.ContainsRune(value, '\\') || bytes.ContainsRune(value, 0) {
		return false
	}
	for _, segment := range strings.Split(string(value), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, character := range segment {
			if unicode.IsControl(character) {
				return false
			}
		}
	}
	return true
}

func validateFileTreeBundle(reader io.Reader) error {
	var header [12]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil || !bytes.Equal(header[:8], fileTreeBundleMagic[:]) {
		return ErrInvalid
	}
	entryCount := binary.LittleEndian.Uint32(header[8:])
	if entryCount > fileTreeMaximumEntries {
		return ErrInvalid
	}
	var previousPath []byte
	for range entryCount {
		var entryHeader [6]byte
		if _, err := io.ReadFull(reader, entryHeader[:]); err != nil {
			return ErrInvalid
		}
		pathLength := binary.LittleEndian.Uint16(entryHeader[:2])
		fileLength := binary.LittleEndian.Uint32(entryHeader[2:])
		if pathLength == 0 || pathLength > fileTreeMaximumPathBytes {
			return ErrInvalid
		}
		pathBytes := make([]byte, pathLength)
		if _, err := io.ReadFull(reader, pathBytes); err != nil || !validFileTreePath(pathBytes) ||
			previousPath != nil && bytes.Compare(previousPath, pathBytes) >= 0 {
			return ErrInvalid
		}
		if _, err := io.CopyN(io.Discard, reader, int64(fileLength)); err != nil {
			return ErrInvalid
		}
		previousPath = pathBytes
	}
	var trailing [1]byte
	if count, err := reader.Read(trailing[:]); count != 0 || err != io.EOF {
		return ErrInvalid
	}
	return nil
}
