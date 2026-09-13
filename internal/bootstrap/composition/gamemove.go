package composition

import (
	"database/sql"

	validationpersistence "retrom/internal/persistence/corevalidation"
	gamemovepersistence "retrom/internal/persistence/gamemove"
	validationservice "retrom/internal/service/corevalidation"
	gamemove "retrom/internal/service/gamemove"
)

// NewGameMove wires the game move application service to its persistence
// adapter and the shared core validation service.
func NewGameMove(database *sql.DB) *gamemove.Service {
	return gamemove.New(
		gamemovepersistence.New(database),
		validationservice.New(validationpersistence.New(database)),
	)
}
