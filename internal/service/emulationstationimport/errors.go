package emulationstationimport

import "errors"

var (
	ErrMapping         = errors.New("EMULATIONSTATION_MAPPING_INCOMPLETE")
	ErrVersionConflict = errors.New("VERSION_CONFLICT")
	ErrNotFound        = errors.New("EMULATIONSTATION_IMPORT_NOT_FOUND")
	ErrActive          = errors.New("EMULATIONSTATION_IMPORT_ACTIVE")
	ErrInvalid         = errors.New("EMULATIONSTATION_IMPORT_INVALID")
)
