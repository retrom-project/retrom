package main

import (
	"bytes"
	"encoding/binary"
	"hash/adler32"
)

func lcfInt(value int32) []byte {
	unsigned := uint32(value)
	shift := 28
	for shift > 0 && unsigned < 1<<shift {
		shift -= 7
	}
	result := make([]byte, 0, shift/7+1)
	for ; shift >= 0; shift -= 7 {
		current := byte(unsigned>>shift) & 0x7f
		if shift > 0 {
			current |= 0x80
		}
		result = append(result, current)
	}
	return result
}

func lcfField(id int32, payload []byte) []byte {
	result := append([]byte{}, lcfInt(id)...)
	result = append(result, lcfInt(int32(len(payload)))...)
	return append(result, payload...)
}

func lcfIntegerField(id, value int32) []byte {
	return lcfField(id, lcfInt(value))
}

func lcfStringField(id int32, value string) []byte {
	return lcfField(id, []byte(value))
}

func lcfStruct(fields ...[]byte) []byte {
	result := make([]byte, 0)
	for _, field := range fields {
		result = append(result, field...)
	}
	return append(result, 0)
}

func lcfStructArray(entries ...[]byte) []byte {
	result := append([]byte{}, lcfInt(int32(len(entries)))...)
	for index, entry := range entries {
		result = append(result, lcfInt(int32(index+1))...)
		result = append(result, entry...)
	}
	return result
}

func littleEndianInt16(values []int16) []byte {
	result := make([]byte, len(values)*2)
	for index, value := range values {
		binary.LittleEndian.PutUint16(result[index*2:], uint16(value))
	}
	return result
}

func zlibStore(contents []byte) []byte {
	var result bytes.Buffer
	result.Write([]byte{0x78, 0x01})
	remaining := contents
	for len(remaining) > 0 {
		blockSize := len(remaining)
		if blockSize > 0xffff {
			blockSize = 0xffff
		}
		final := blockSize == len(remaining)
		if final {
			result.WriteByte(1)
		} else {
			result.WriteByte(0)
		}
		length := uint16(blockSize)
		_ = binary.Write(&result, binary.LittleEndian, length)
		_ = binary.Write(&result, binary.LittleEndian, ^length)
		result.Write(remaining[:blockSize])
		remaining = remaining[blockSize:]
	}
	if len(contents) == 0 {
		result.Write([]byte{1, 0, 0, 0xff, 0xff})
	}
	_ = binary.Write(&result, binary.BigEndian, adler32.Checksum(contents))
	return result.Bytes()
}
