package saves

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func fileTreeBundle() []byte {
	entries := []struct {
		path       string
		pathLength uint16
		data       []byte
		dataLength uint32
	}{
		{path: "GAME/DATA.BIN", pathLength: 13, data: []byte{1, 2, 3}, dataLength: 3},
		{path: "GAME/ICON0.PNG", pathLength: 14, data: []byte{4, 5}, dataLength: 2},
	}
	result := bytes.NewBuffer(nil)
	result.Write(fileTreeBundleMagic[:])
	_ = binary.Write(result, binary.LittleEndian, uint32(2))
	for _, entry := range entries {
		_ = binary.Write(result, binary.LittleEndian, entry.pathLength)
		_ = binary.Write(result, binary.LittleEndian, entry.dataLength)
		result.WriteString(entry.path)
		result.Write(entry.data)
	}
	return result.Bytes()
}

func TestValidateFileTreeBundle(t *testing.T) {
	t.Parallel()
	valid := fileTreeBundle()
	if err := validateFileTreeBundle(bytes.NewReader(valid)); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}

	tests := map[string][]byte{
		"trailing byte": append(append([]byte(nil), valid...), 0),
		"truncated":     valid[:len(valid)-1],
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validateFileTreeBundle(bytes.NewReader(body)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestValidateFileTreeBundleRejectsUnsafeAndNonCanonicalPaths(t *testing.T) {
	t.Parallel()
	for name, paths := range map[string][]struct {
		path   string
		length uint16
	}{
		"traversal": {{path: "../escape", length: 9}, {path: "GAME/ICON0.PNG", length: 14}},
		"duplicate": {{path: "GAME/DATA.BIN", length: 13}, {path: "GAME/DATA.BIN", length: 13}},
		"unsorted":  {{path: "GAME/ICON0.PNG", length: 14}, {path: "GAME/DATA.BIN", length: 13}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := bytes.NewBuffer(nil)
			body.Write(fileTreeBundleMagic[:])
			_ = binary.Write(body, binary.LittleEndian, uint32(2))
			for _, entry := range paths {
				_ = binary.Write(body, binary.LittleEndian, entry.length)
				_ = binary.Write(body, binary.LittleEndian, uint32(1))
				body.WriteString(entry.path)
				body.WriteByte(1)
			}
			if err := validateFileTreeBundle(bytes.NewReader(body.Bytes())); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}
