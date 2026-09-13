package composition

import (
	"database/sql"

	homepersistence "retrom/internal/persistence/home"
	homeservice "retrom/internal/service/home"
	"retrom/internal/service/tagging"
)

func NewHome(database *sql.DB, tags *tagging.Service) *homeservice.Service {
	return homeservice.New(homepersistence.New(database), tags)
}
