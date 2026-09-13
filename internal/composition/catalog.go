package composition

import (
	"database/sql"

	catalogpersistence "retrom/internal/persistence/catalog"
	catalogservice "retrom/internal/service/catalog"
	"retrom/internal/service/netplay"
)

// NewCatalog wires catalog read use cases to the database adapter and the
// optional netplay capability registry used by platform projections.
func NewCatalog(database *sql.DB, netplayService *netplay.Service) *catalogservice.Service {
	return catalogservice.New(catalogpersistence.New(database), netplayService)
}
