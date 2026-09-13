package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/persistence/blobregistry"
	application "retrom/internal/service/payloadrelease"
)

type (
	Lifecycle       struct{ database *sql.DB }
	lifecycleReader struct{ executor dbexec.Executor }
)

func NewLifecycle(database *sql.DB) *Lifecycle { return &Lifecycle{database: database} }

func (repository *Lifecycle) WithLifecycle(ctx context.Context, run func(application.LifecycleReader) error) error {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin lifecycle snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := run(lifecycleReader{tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit lifecycle snapshot: %w", err)
	}
	return nil
}

func (lifecycleReader) BlobEdges(ctx context.Context) ([]application.BlobEdge, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read lifecycle registry: %w", err)
	}
	return LoadLifecycleBlobEdges()
}

func LoadLifecycleBlobEdges() ([]application.BlobEdge, error) {
	edges, err := blobregistry.Load()
	if err != nil {
		return nil, fmt.Errorf("read lifecycle blob registry: %w", err)
	}
	result := make([]application.BlobEdge, 0, len(edges))
	for _, edge := range edges {
		result = append(result, application.BlobEdge{Table: edge.Table, Column: edge.Column})
	}
	return result, nil
}

func (reader lifecycleReader) Owners(
	ctx context.Context, cursor application.Scope, limit int,
) ([]application.LifecycleOwner, error) {
	rows, err := reader.executor.QueryContext(ctx, lifecycleOwnersQuery, cursor.Type, cursor.Type, cursor.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("read lifecycle owner snapshot: %w", err)
	}
	defer func() { cleanup.Error("close lifecycle owners", rows.Close()) }()
	result := make([]application.LifecycleOwner, 0)
	for rows.Next() {
		var owner application.LifecycleOwner
		err := rows.Scan(&owner.Owner.Scope.Type, &owner.Owner.Scope.ID, &owner.Owner.State, &owner.Owner.Version,
			&owner.Owner.PayloadState, &owner.Owner.ReleaseJobID, &owner.Owner.PublicID, &owner.Owner.Retryable,
			&owner.ReleaseJobID, &owner.ReleaseKind, &owner.ReleaseScope.Type, &owner.ReleaseScope.ID, &owner.PublicReleaseJobID)
		if err != nil {
			return nil, fmt.Errorf("scan lifecycle owner: %w", err)
		}
		result = append(result, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lifecycle owners: %w", err)
	}
	return result, nil
}
