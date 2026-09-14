package firmware

import (
	"errors"

	firmwarecapability "retrom/internal/capability/content/firmware"
)

var (
	ErrInvalid              = firmwarecapability.ErrInvalid
	ErrArchiveFactsNotFound = errors.New("BIOS_ARCHIVE_FACTS_NOT_FOUND")
	ErrCatalogChanged       = errors.New("BIOS_REQUIREMENT_CATALOG_CHANGED")
)
