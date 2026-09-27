package composition

import (
	dbapi "retrom/internal/database"

	readinesspersistence "retrom/internal/persistence/readiness"
	readinessservice "retrom/internal/service/readiness"
)

func NewReadiness(database dbapi.DB) *readinessservice.Service {
	return readinessservice.New(readinesspersistence.New(database))
}
