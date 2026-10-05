// Package nintendo3ds reads bounded NCSD/NCCH container facts. It does not
// select a runtime, decrypt content, or handle keys.
package nintendo3ds

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var ErrInvalid = errors.New("THREEDS_CONTAINER_INVALID")

const headerBytes int64 = 512

type Partition struct {
	Index      int   `json:"index"`
	Offset     int64 `json:"offset"`
	Size       int64 `json:"size"`
	Encrypted  bool  `json:"encrypted"`
	Executable bool  `json:"executable"`
}
type Facts struct {
	Format     string      `json:"format"`
	Partitions []Partition `json:"partitions"`
}

func Read(reader io.ReaderAt, size int64) (Facts, error) {
	header, err := readHeader(reader, 0, size)
	if err != nil {
		return Facts{}, err
	}
	switch string(header[0x100:0x104]) {
	case "NCCH":
		part, err := readNCCH(header, 0, 0, size)
		if err != nil {
			return Facts{}, err
		}
		return Facts{Format: "NCCH", Partitions: []Partition{part}}, nil
	case "NCSD":
		return readNCSD(reader, header, size)
	default:
		return Facts{}, ErrInvalid
	}
}

func readHeader(reader io.ReaderAt, offset, size int64) ([]byte, error) {
	if offset < 0 || size < headerBytes || offset > size-headerBytes {
		return nil, ErrInvalid
	}
	header := make([]byte, headerBytes)
	if _, err := reader.ReadAt(header, offset); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return header, nil
}

func units(data []byte, scale int64) int64 { return int64(binary.LittleEndian.Uint32(data)) * scale }

func readNCSD(reader io.ReaderAt, header []byte, size int64) (Facts, error) {
	result := Facts{Format: "NCSD"}
	mediaSize := units(header[0x104:], headerBytes)
	if mediaSize < headerBytes {
		return Facts{}, ErrInvalid
	}
	for index := range 8 {
		start := 0x120 + index*8
		offset, length := units(header[start:], headerBytes), units(header[start+4:], headerBytes)
		if offset == 0 && length == 0 {
			continue
		}
		if !validPartitionExtent(offset, length, size, mediaSize, result.Partitions) {
			return Facts{}, ErrInvalid
		}
		inner, err := readHeader(reader, offset, size)
		if err != nil {
			return Facts{}, err
		}
		part, err := readNCCH(inner, index, offset, length)
		if err != nil {
			return Facts{}, err
		}
		// Keep the outer extent for overlap checks, including any partition padding.
		part.Size = length
		result.Partitions = append(result.Partitions, part)
	}
	if len(result.Partitions) == 0 || result.Partitions[0].Index != 0 || !result.Partitions[0].Executable {
		return Facts{}, ErrInvalid
	}
	return result, nil
}

func readNCCH(header []byte, index int, offset, size int64) (Partition, error) {
	if string(header[0x100:0x104]) != "NCCH" || header[0x18e] > 31 {
		return Partition{}, ErrInvalid
	}
	unit := headerBytes << header[0x18e]
	// Reject before multiplication; header fields are unsigned 32-bit units.
	count := int64(binary.LittleEndian.Uint32(header[0x104:]))
	if count == 0 || count > size/unit {
		return Partition{}, ErrInvalid
	}
	length := count * unit
	if length < headerBytes {
		return Partition{}, ErrInvalid
	}
	extSize := int64(binary.LittleEndian.Uint32(header[0x180:]))
	if extSize > length-headerBytes {
		return Partition{}, ErrInvalid
	}
	// Plain, logo, ExeFS and RomFS extents share NCCH media-unit addressing.
	for _, start := range []int{0x190, 0x198, 0x1a0, 0x1b0} {
		at, n := int64(binary.LittleEndian.Uint32(header[start:])), int64(binary.LittleEndian.Uint32(header[start+4:]))
		if n == 0 {
			continue
		}
		if at == 0 || at > length/unit || n > length/unit-at {
			return Partition{}, ErrInvalid
		}
	}
	return Partition{
		Index: index, Offset: offset, Size: length,
		Encrypted: header[0x18f]&4 == 0, Executable: header[0x18d]&2 != 0,
	}, nil
}

func validPartitionExtent(offset, length, size, mediaSize int64, prior []Partition) bool {
	if offset < headerBytes || length < headerBytes || length > size || offset > size-length || offset > mediaSize-length {
		return false
	}
	for _, part := range prior {
		if offset < part.Offset+part.Size && part.Offset < offset+length {
			return false
		}
	}
	return true
}
