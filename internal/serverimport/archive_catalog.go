package serverimport

func rejectedArchiveOutcome(candidates []*evaluatedCandidate) (string, string) {
	for _, candidate := range candidates {
		if candidate.Item.ArchiveMembersJSON != nil && candidate.DAT != nil && candidate.DAT.Status == "INVALID" {
			return "INVALID_ARCHIVE", "BIOS_ARCHIVE_CONTENT_MISMATCH"
		}
	}
	return "READ_FAILED", "SERVER_IMPORT_SOURCE_UNREADABLE"
}
