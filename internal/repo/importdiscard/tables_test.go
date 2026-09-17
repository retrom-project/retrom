package importdiscard_test

import "retrom/internal/model/importdiscard"

func batchTable(kind string) (string, error) {
	switch kind {
	case "IMPORT":
		return "import_jobs", nil
	case "PEGASUS":
		return "pegasus_imports", nil
	case "EMULATIONSTATION":
		return "emulationstation_imports", nil
	default:
		return "", importdiscard.ErrInvalid
	}
}
