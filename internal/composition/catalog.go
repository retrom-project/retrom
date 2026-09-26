package composition

import (
	dbapi "retrom/internal/database"

	catalogpersistence "retrom/internal/persistence/catalog"
	catalogservice "retrom/internal/service/catalog"
)

// NewCatalog wires catalog read use cases to the database adapter and the
// platform projections.
func NewCatalog(database dbapi.DB) *catalogservice.Service {
	return catalogservice.New(catalogpersistence.New(database))
}
