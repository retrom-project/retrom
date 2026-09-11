package firmware

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/firmwaremanifest"
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

func loadStaticArchiveMembers(
	ctx context.Context, queryer archiveQueryer, requirementID string,
) ([]expectedArchiveEntry, bool, error) {
	var value sql.NullString
	if err := queryer.QueryRowContext(ctx,
		"SELECT archive_members_json FROM bios_requirements WHERE id=?", requirementID).Scan(&value); err != nil {
		return nil, false, fmt.Errorf("load firmware archive declaration: %w", err)
	}
	if !value.Valid {
		return nil, false, nil
	}
	members, err := StaticArchiveExpectations(value.String)
	if err != nil {
		return nil, true, err
	}
	result := make([]expectedArchiveEntry, 0, len(members))
	for _, member := range members {
		result = append(result, expectedArchiveEntry{
			name: member.Name, size: member.SizeBytes,
			crc32: sql.NullString{String: member.CRC32, Valid: true},
			sha1:  sql.NullString{String: member.SHA1, Valid: true},
		})
	}
	return result, true, nil
}

func isStaticArchive(ctx context.Context, queryer archiveQueryer, requirementID string) (bool, error) {
	var strict bool
	err := queryer.QueryRowContext(ctx,
		"SELECT archive_members_json IS NOT NULL FROM bios_requirements WHERE id=?", requirementID).Scan(&strict)
	if err != nil {
		return false, fmt.Errorf("read firmware archive policy: %w", err)
	}
	return strict, nil
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
