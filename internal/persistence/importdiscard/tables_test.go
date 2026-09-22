package importdiscard_test

import "retrom/internal/service/importdiscard"

func batchTable(kind string) (string, error) {
	switch kind {
	case "IMPORT":
		return "import_jobs", nil
	case "SOURCE":
		return "source_imports", nil
	default:
		return "", importdiscard.ErrInvalid
	}
}
