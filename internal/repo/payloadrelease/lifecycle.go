package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/blobregistry"
)

type Lifecycle struct{ database *sql.DB }

func NewLifecycle(database *sql.DB) *Lifecycle { return &Lifecycle{database: database} }

func (Lifecycle) BlobEdges(ctx context.Context) ([]application.BlobEdge, error) {
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

func (repository *Lifecycle) Owners(
	ctx context.Context, cursor application.Scope, limit int,
) ([]application.LifecycleOwner, error) {
	rows, err := repository.database.QueryContext(ctx, lifecycleOwnersQuery, cursor.Type, cursor.Type, cursor.ID, limit)
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
