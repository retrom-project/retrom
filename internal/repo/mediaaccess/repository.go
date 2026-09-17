package mediaaccess

import (
	"context"
	"database/sql"

	service "retrom/internal/model/mediaaccess"
	"retrom/internal/repo/dbexec"
)

type (
	Repository struct{ database *sql.DB }
	reader     struct{ executor dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) Game(ctx context.Context, id string) (service.GameAsset, bool, error) {
	return reader{executor: repository.database}.Game(ctx, id)
}

func (repository *Repository) Save(ctx context.Context, id string) (service.SaveScreenshot, bool, error) {
	return reader{executor: repository.database}.Save(ctx, id)
}

func (repository *Repository) Review(ctx context.Context, id string) ([]service.ReviewAsset, error) {
	return reader{executor: repository.database}.Review(ctx, id)
}

func (repository *Repository) Sources(ctx context.Context, id, kind string) ([]service.ReviewAsset, error) {
	return reader{executor: repository.database}.Sources(ctx, id, kind)
}
