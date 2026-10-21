package model

import (
	"path"
	"strings"
	"unicode/utf8"
)

const (
	MaximumContentMembers       = 50_000
	MaximumMemberSize     int64 = 8 * 1024 * 1024 * 1024
	MaximumContentSize    int64 = 64 * 1024 * 1024 * 1024
	MaximumPackageSize          = MaximumContentSize + MaximumContentMembers*(2*1024+256) + 1024
)

func SafeContentPath(value string) bool {
	if value == "" || value == "." || len(value) > 1024 || !utf8.ValidString(value) ||
		strings.ContainsAny(value, "\\\x00") ||
		strings.HasPrefix(value, "/") || path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	return !strings.ContainsFunc(value, func(character rune) bool { return character < 32 || character == 127 })
}

func ValidatePackage(file GameFile) error {
	if file.Role != "project_package" {
		if len(file.Members) != 0 {
			return ErrInvalid
		}
		return nil
	}
	if len(file.Members) == 0 || len(file.Members) > MaximumContentMembers || !Hash(file.SHA256) ||
		file.SizeBytes <= 0 || file.SizeBytes > MaximumPackageSize {
		return ErrInvalid
	}
	var total int64
	for _, member := range file.Members {
		if !validPackageMember(member, file.SizeBytes) {
			return ErrInvalid
		}
		total += member.SizeBytes
		if total > MaximumContentSize {
			return ErrInvalid
		}
	}
	return nil
}

func validPackageMember(member GameFileMember, objectSize int64) bool {
	if !SafeContentPath(member.LogicalPath) || !Hash(member.SHA256) || member.MediaType == "" ||
		len(member.MediaType) > 255 {
		return false
	}
	if member.DataOffset < 0 || member.DataOffset > objectSize || member.SizeBytes < 0 ||
		member.SizeBytes > MaximumMemberSize ||
		member.SizeBytes > objectSize-member.DataOffset {
		return false
	}
	return true
}
