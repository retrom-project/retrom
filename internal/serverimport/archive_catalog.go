package serverimport

import (
	"database/sql"
	"fmt"

	"retrom/internal/firmware"
)

func (item catalogItem) isArchive() bool {
	return item.SourceKind == "DAT_MACHINE" || item.ArchiveMembersJSON != nil
}

func (item catalogItem) validateSource(datStatus sql.NullString, datActive sql.NullInt64) error {
	if item.ArchiveMembersJSON != nil {
		if _, err := firmware.StaticArchiveExpectations(*item.ArchiveMembersJSON); err != nil {
			return fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
		}
	}
	if !item.isArchive() && item.ExpectedMD5 == nil && item.ExpectedSHA1 == nil &&
		item.ExpectedSHA256 == nil && (item.ExpectedSize == nil || *item.ExpectedSize <= 0) {
		return ErrCatalogInvalid
	}
	if item.SourceKind == "DAT_MACHINE" &&
		(item.DATVersionID == nil || !datStatus.Valid || datStatus.String != "READY" ||
			!datActive.Valid || datActive.Int64 != 1) {
		return ErrCatalogInvalid
	}
	return nil
}

func rejectedArchiveOutcome(candidates []*evaluatedCandidate) (string, string) {
	for _, candidate := range candidates {
		if candidate.Item.ArchiveMembersJSON != nil && candidate.DAT != nil && candidate.DAT.Status == "INVALID" {
			return "INVALID_ARCHIVE", "BIOS_ARCHIVE_CONTENT_MISMATCH"
		}
	}
	return "READ_FAILED", "SERVER_IMPORT_SOURCE_UNREADABLE"
}

func (item catalogItem) applyArchivePolicy(evaluation *firmware.DATEvaluation) {
	if item.ArchiveMembersJSON != nil {
		firmware.RequireCompleteArchive(evaluation)
	}
}
