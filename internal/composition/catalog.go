package composition

import (
	"database/sql"

	catalogpersistence "retrom/internal/persistence/catalog"
	catalogservice "retrom/internal/service/catalog"
)

// NewCatalog wires catalog read use cases to the database adapter and the
// platform projections.
func NewCatalog(database *sql.DB) *catalogservice.Service {
	return catalogservice.New(catalogpersistence.New(database))
}
