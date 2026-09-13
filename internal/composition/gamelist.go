package composition

import (
	"database/sql"

	gamelistpersistence "retrom/internal/persistence/gamelist"
	gamelistservice "retrom/internal/service/gamelist"
)

// NewGameList wires the game list application service to the database adapter.
func NewGameList(database *sql.DB) *gamelistservice.Service {
	return gamelistservice.New(gamelistpersistence.New(database))
}
