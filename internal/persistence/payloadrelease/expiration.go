package payloadrelease

import (
	"context"
	"fmt"
	dbapi "retrom/internal/database"
	preview "retrom/internal/persistence/libraryimport/payloadpreview"
	provider "retrom/internal/persistence/metadatascrape/payloadprovider"
	application "retrom/internal/service/payloadrelease"
)

type Expiration struct{ database dbapi.DB }

func NewExpiration(database dbapi.DB) *Expiration { return &Expiration{database: database} }

func (repository *Expiration) WithExpiration(ctx context.Context, run func(application.ExpirationScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin payload expiration: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := expirationRecords{provider: provider.Records{Executor: tx}, preview: preview.Records{Executor: tx}}
	if err := run(application.ExpirationScope{Read: records, Write: records, GC: BindGC(tx)}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit payload expiration: %w", err)
	}
	return nil
}

type expirationRecords struct {
	provider provider.Records
	preview  preview.Records
}

func (records expirationRecords) Providers(ctx context.Context, now int64, limit int) ([]application.ProviderExpiration, error) {
	return records.provider.Providers(ctx, now, limit)
}
func (records expirationRecords) ReleaseProvider(ctx context.Context, before application.ProviderExpiration, now int64) error {
	return records.provider.ReleaseProvider(ctx, before, now)
}
func (records expirationRecords) Previews(ctx context.Context, now int64, limit int) ([]application.PreviewExpiration, error) {
	return records.preview.Previews(ctx, now, limit)
}
func (records expirationRecords) ExpirePreview(ctx context.Context, change application.PreviewExpiry) error {
	return records.preview.ExpirePreview(ctx, change)
}
