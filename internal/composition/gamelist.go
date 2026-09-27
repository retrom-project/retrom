package composition

import (
	dbapi "retrom/internal/database"

	gamelistpersistence "retrom/internal/persistence/gamelist"
	gamelistservice "retrom/internal/service/gamelist"
)

// NewGameList wires the game list application service to the database adapter.
func NewGameList(database dbapi.DB) *gamelistservice.Service {
	return gamelistservice.New(gamelistpersistence.New(database))
}
