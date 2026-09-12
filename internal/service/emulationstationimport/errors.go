package emulationstationimport

import "errors"

var (
	ErrNotFound = errors.New("EMULATIONSTATION_IMPORT_NOT_FOUND")
	ErrInvalid  = errors.New("EMULATIONSTATION_IMPORT_INVALID")
)
