package nintendo3ds

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func ncch(decrypted bool) []byte {
	data := make([]byte, 4096)
	copy(data[0x100:], "NCCH")
	binary.LittleEndian.PutUint32(data[0x104:], 8)
	data[0x18d] = 2 // executable
	if decrypted {
		data[0x18f] = 4
	}
	binary.LittleEndian.PutUint32(data[0x1a0:], 1)
	binary.LittleEndian.PutUint32(data[0x1a4:], 1)
	return data
}

func ncsd(decrypted bool) []byte {
	data := make([]byte, 8192)
	copy(data[0x100:], "NCSD")
	binary.LittleEndian.PutUint32(data[0x104:], 16)
	binary.LittleEndian.PutUint32(data[0x120:], 8)
	binary.LittleEndian.PutUint32(data[0x124:], 8)
	copy(data[4096:], ncch(decrypted))
	return data
}

func TestParseHeaderFacts(t *testing.T) {
	for _, container := range []string{"NCCH", "NCSD"} {
		for _, decrypted := range []bool{false, true} {
			data := ncch(decrypted)
			if container == "NCSD" {
				data = ncsd(decrypted)
			}
			facts, err := Read(bytes.NewReader(data), int64(len(data)))
			if err != nil || facts.Format != container || len(facts.Partitions) != 1 || facts.Partitions[0].Encrypted == decrypted || !facts.Partitions[0].Executable {
				t.Fatalf("%s %v: %+v %v", container, decrypted, facts, err)
			}
		}
	}
}

func TestRejectMalformedBounds(t *testing.T) {
	cases := map[string]func([]byte) []byte{
		"truncated header":                func(data []byte) []byte { return data[:500] },
		"missing main":                    func(data []byte) []byte { clear(data[0x120:0x128]); return data },
		"partition overflow":              func(data []byte) []byte { binary.LittleEndian.PutUint32(data[0x120:], 0xffffffff); return data },
		"partition overlap":               func(data []byte) []byte { copy(data[0x128:0x130], data[0x120:0x128]); return data },
		"invalid inner magic":             func(data []byte) []byte { data[4096+0x100] = 0; return data },
		"inner content exceeds partition": func(data []byte) []byte { binary.LittleEndian.PutUint32(data[4096+0x104:], 9); return data },
		"exefs exceeds partition":         func(data []byte) []byte { binary.LittleEndian.PutUint32(data[4096+0x1a0:], 8); return data },
		"excessive unit exponent":         func(data []byte) []byte { data[4096+0x18e] = 255; return data },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			data := mutate(ncsd(true))
			_, err := Read(bytes.NewReader(data), int64(len(data)))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

type boundedReader struct {
	reads, bytes int
	reader       *bytes.Reader
}

func (reader *boundedReader) ReadAt(data []byte, offset int64) (int, error) {
	reader.reads++
	reader.bytes += len(data)
	return reader.reader.ReadAt(data, offset)
}

func TestOnlyBoundedHeadersAreRead(t *testing.T) {
	data := ncsd(true)
	reader := &boundedReader{reader: bytes.NewReader(data)}
	if _, err := Read(reader, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if reader.reads != 2 || reader.bytes != 1024 {
		t.Fatalf("unbounded content read: %+v", reader)
	}
}

func TestUnusedRegionOffsetHasNoExtent(t *testing.T) {
	data := ncch(true)
	binary.LittleEndian.PutUint32(data[0x198:], 5)
	if _, err := Read(bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
}
