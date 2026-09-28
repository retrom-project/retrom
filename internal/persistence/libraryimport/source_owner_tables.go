package libraryimport

import libraryservice "retrom/internal/service/libraryimport"

func sourceOwnerTable(kind libraryservice.SourceOwnerKind) (string, error) {
	switch kind {
	case libraryservice.SourceOwnerSource:
		return "source_import_items", nil

	default:
		return "", libraryservice.ErrInvalid
	}
}

func sourceOwnerFilesTable(kind libraryservice.SourceOwnerKind) (string, error) {
	switch kind {
	case libraryservice.SourceOwnerSource:
		return "source_import_item_files", nil

	default:
		return "", libraryservice.ErrInvalid
	}
}
