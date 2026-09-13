package composition

import (
	"database/sql"

	readinesspersistence "retrom/internal/persistence/readiness"
	readinessservice "retrom/internal/service/readiness"
)

func NewReadiness(database *sql.DB) *readinessservice.Service {
	return readinessservice.New(readinesspersistence.New(database))
}
