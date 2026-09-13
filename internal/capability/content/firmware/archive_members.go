package firmware

import (
	"fmt"

	"retrom/internal/capability/content/firmwaremanifest"
)

func StaticArchiveExpectations(value string) ([]ExpectedDATEntry, error) {
	members, err := firmwaremanifest.DecodeMembers(value)
	if err != nil {
		return nil, fmt.Errorf("read static archive expectations: %w", err)
	}
	result := make([]ExpectedDATEntry, 0, len(members))
	for _, member := range members {
		if member.Required {
			result = append(result, ExpectedDATEntry{
				Name: member.Name, SizeBytes: member.SizeBytes, CRC32: member.CRC32, SHA1: member.SHA1,
			})
		}
	}
	return result, nil
}

// Static core archives must supply the selected BIOS and all common MCU ROMs.
// DAT archives retain their existing advisory findings and CRC alias handling.
func RequireCompleteArchive(value *DATEvaluation) {
	if value.MissingCount > 0 || value.MismatchedCount > 0 || value.AliasedCount > 0 {
		value.Status, value.Launchable = "INVALID", false
	}
}

// ArchiveContentError reports required member failures without replacing an active installation.
type ArchiveContentError struct{ Details map[string]any }

func (err *ArchiveContentError) Error() string {
	return "BIOS archive required members are incomplete or mismatched"
}
func (err *ArchiveContentError) Unwrap() error { return ErrInvalid }
