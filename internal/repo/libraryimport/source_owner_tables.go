package libraryimport

import application "retrom/internal/model/libraryimport"

func sourceOwnerTable(kind application.SourceOwnerKind) (string, error) {
	switch kind {
	case application.SourceOwnerPegasus:
		return "pegasus_import_items", nil
	case application.SourceOwnerEmulationStation:
		return "emulationstation_import_items", nil
	default:
		return "", application.ErrInvalid
	}
}

func sourceOwnerFilesTable(kind application.SourceOwnerKind) (string, error) {
	switch kind {
	case application.SourceOwnerPegasus:
		return "pegasus_import_item_files", nil
	case application.SourceOwnerEmulationStation:
		return "emulationstation_import_item_files", nil
	default:
		return "", application.ErrInvalid
	}
}
