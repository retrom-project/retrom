package composition

import (
	dbapi "retrom/internal/database"

	biospersistence "retrom/internal/persistence/bios"
	biosservice "retrom/internal/service/bios"
)

// NewBIOS wires the BIOS catalog application service to its database adapter.
func NewBIOS(database dbapi.DB) *biosservice.Service {
	return biosservice.New(biospersistence.New(database))
}
