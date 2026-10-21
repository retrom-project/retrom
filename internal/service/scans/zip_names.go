package scans

import (
	"archive/zip"
	"encoding/binary"
	"hash/crc32"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"retrom/internal/model"
)

// Explicit ZIP encoding metadata takes precedence over guessing from valid UTF-8 bytes.
func zipMemberName(member *zip.File) (string, error) {
	if member.Flags&0x800 != 0 {
		if !utf8.ValidString(member.Name) {
			return "", model.ErrInvalid
		}
		return member.Name, nil
	}
	name, err := zipUnicodePath(member.Name, member.Extra)
	if err != nil || name != "" {
		return name, err
	}
	if utf8.ValidString(member.Name) {
		return member.Name, nil
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes([]byte(member.Name))
	if err != nil {
		return "", model.ErrInvalid
	}
	return string(decoded), nil
}

func zipUnicodePath(raw string, extra []byte) (string, error) {
	for len(extra) > 0 {
		if len(extra) < 4 {
			return "", model.ErrInvalid
		}
		tag := binary.LittleEndian.Uint16(extra)
		size := int(binary.LittleEndian.Uint16(extra[2:]))
		extra = extra[4:]
		if size > len(extra) {
			return "", model.ErrInvalid
		}
		field := extra[:size]
		extra = extra[size:]
		if tag != 0x7075 || len(field) < 5 || field[0] != 1 {
			continue
		}
		if binary.LittleEndian.Uint32(field[1:]) != crc32.ChecksumIEEE([]byte(raw)) {
			continue
		}
		if len(field) == 5 || !utf8.Valid(field[5:]) {
			return "", model.ErrInvalid
		}
		return string(field[5:]), nil
	}
	return "", nil
}
