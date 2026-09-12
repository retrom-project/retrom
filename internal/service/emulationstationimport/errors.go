package emulationstationimport

import "errors"

var (
	ErrNoSelection          = errors.New("EMULATIONSTATION_NO_COLLECTION_SELECTED")
	ErrSourceChanged        = errors.New("EMULATIONSTATION_SOURCE_CHANGED")
	ErrMappingTargetChanged = errors.New("EMULATIONSTATION_MAPPING_TARGET_CHANGED")
	ErrExpired              = errors.New("EMULATIONSTATION_PLAN_EXPIRED")
	ErrMapping              = errors.New("EMULATIONSTATION_MAPPING_INCOMPLETE")
	ErrVersionConflict      = errors.New("VERSION_CONFLICT")
	ErrNotFound             = errors.New("EMULATIONSTATION_IMPORT_NOT_FOUND")
	ErrActive               = errors.New("EMULATIONSTATION_IMPORT_ACTIVE")
	ErrInvalid              = errors.New("EMULATIONSTATION_IMPORT_INVALID")
)
