package composition

import (
	"database/sql"

	biospersistence "retrom/internal/repo/bios"
	biosservice "retrom/internal/service/bios"
)

// NewBIOS wires the BIOS catalog application service to its database adapter.
func NewBIOS(database *sql.DB) *biosservice.Service {
	return biosservice.New(biospersistence.New(database))
}
