package serverimport

func rejectedArchiveOutcome(candidates []*evaluatedCandidate) (string, string) {
	for _, candidate := range candidates {
		switch candidate.State {
		case "CATALOG_INVALID", "VALIDATION_FAILED", "INVALID_ARCHIVE", "ARCHIVE_UNSAFE", "SOURCE_CHANGED":
			state := candidate.State
			if state == "ARCHIVE_UNSAFE" {
				state = "INVALID_ARCHIVE"
			}
			if code, ok := candidate.Details["code"].(string); ok {
				return state, code
			}
		}
		if candidate.Item.ArchiveMembersJSON != nil && candidate.DAT != nil && candidate.DAT.Status == "INVALID" {
			return "INVALID_ARCHIVE", "BIOS_ARCHIVE_CONTENT_MISMATCH"
		}
	}
	return "READ_FAILED", "SERVER_IMPORT_SOURCE_UNREADABLE"
}
