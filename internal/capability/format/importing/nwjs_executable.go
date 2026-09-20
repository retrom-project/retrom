package importing

import (
	"encoding/binary"
	"errors"
)

var ErrNWJSExecutableInvalid = errors.New("NWJS_EXECUTABLE_INVALID")

const maximumPEHeaderOffset = 16 << 20

func PEHeaderOffset(header [64]byte, size int64) (int64, error) {
	if string(header[:2]) != "MZ" {
		return 0, ErrNWJSExecutableInvalid
	}
	offset := int64(binary.LittleEndian.Uint32(header[0x3c:]))
	if offset < int64(len(header)) || offset > maximumPEHeaderOffset || offset > size-24 {
		return 0, ErrNWJSExecutableInvalid
	}
	return offset, nil
}

func PEMinimumZIPOffset(header [24]byte, offset int64) (int64, error) {
	if string(header[:4]) != "PE\x00\x00" {
		return 0, ErrNWJSExecutableInvalid
	}
	machine := binary.LittleEndian.Uint16(header[4:6])
	sections := binary.LittleEndian.Uint16(header[6:8])
	if !supportedPEMachine(machine) || sections == 0 || sections > 96 {
		return 0, ErrNWJSExecutableInvalid
	}
	return offset + int64(len(header)), nil
}

func supportedPEMachine(machine uint16) bool {
	return machine == 0x014c || machine == 0x8664 || machine == 0xaa64
}
