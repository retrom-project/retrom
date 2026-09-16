package composition

import (
	"database/sql"
	"time"

	tagpersistence "retrom/internal/repo/tagging"
	tagging "retrom/internal/service/tagging"
)

func newTagService(database *sql.DB, now func() time.Time) *tagging.Service {
	repo := tagpersistence.New(database)
	return tagging.New(repo, repo, tagging.Options{Now: now})
}
