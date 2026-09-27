package composition

import (
	"time"

	dbapi "retrom/internal/database"

	repository "retrom/internal/persistence/gamemetadata"
	payloadservice "retrom/internal/service/cleanupjobs"
	application "retrom/internal/service/gamemetadata"
)

// NewGameMetadata wires the game metadata application service to its
// persistence adapter. Keeping this constructor in composition lets HTTP
// adapters depend on the service boundary without importing SQL repositories.
func NewGameMetadata(
	database dbapi.DB, deletion payloadservice.DeletionStager, now func() time.Time,
) *application.Service {
	return application.New(repository.New(database, deletion), now)
}
