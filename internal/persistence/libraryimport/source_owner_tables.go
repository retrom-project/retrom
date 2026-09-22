package libraryimport

import application "retrom/internal/service/libraryimport"

func sourceOwnerTable(kind application.SourceOwnerKind) (string, error) {
	switch kind {
	case application.SourceOwnerSource:
		return "source_import_items", nil

	default:
		return "", application.ErrInvalid
	}
}

func sourceOwnerFilesTable(kind application.SourceOwnerKind) (string, error) {
	switch kind {
	case application.SourceOwnerSource:
		return "source_import_item_files", nil

	default:
		return "", application.ErrInvalid
	}
}
