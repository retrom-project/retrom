package composition

import (
	"database/sql"
	"time"

	payloadservice "retrom/internal/model/payloadrelease"
	repository "retrom/internal/repo/gamemetadata"
	application "retrom/internal/service/gamemetadata"
)

// NewGameMetadata wires the game metadata application service to its
// persistence adapter. Keeping this constructor in composition lets HTTP
// adapters depend on the service boundary without importing SQL repositories.
func NewGameMetadata(
	database *sql.DB, gc payloadservice.GCStager, now func() time.Time,
) *application.Service {
	return application.New(repository.New(database, gc), now)
}
