package emulationstationimport

import "errors"

var (
	ErrNotFound = errors.New("EMULATIONSTATION_IMPORT_NOT_FOUND")
	ErrActive   = errors.New("EMULATIONSTATION_IMPORT_ACTIVE")
	ErrInvalid  = errors.New("EMULATIONSTATION_IMPORT_INVALID")
)
