package gamecontent

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/gamecontent"
)

type ImpactQueries struct{ database dbapi.DB }

func NewImpactQueries(database dbapi.DB) *ImpactQueries { return &ImpactQueries{database: database} }

type impactRecords struct{ executor dbapi.Executor }

func BindImpact(executor dbapi.Executor) application.ImpactReader {
	return impactRecords{executor: executor}
}

func (repository *ImpactQueries) ReadImpact(ctx context.Context, gameID string) (application.ImpactSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return application.ImpactSnapshot{}, fmt.Errorf("begin game impact snapshot: %w", err)
	}
	defer dbapi.Rollback(tx)
	result, err := BindImpact(tx).ReadImpact(ctx, gameID)
	if err != nil {
		return application.ImpactSnapshot{}, fmt.Errorf("read game impact snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.ImpactSnapshot{}, fmt.Errorf("commit game impact snapshot: %w", err)
	}
	return result, nil
}

func (records impactRecords) ReadImpact(ctx context.Context, gameID string) (application.ImpactSnapshot, error) {
	ids, err := gameImpactBlobIDs(ctx, records.executor, gameID)
	if err != nil {
		return application.ImpactSnapshot{}, err
	}
	result := application.ImpactSnapshot{GameID: gameID}
	for _, id := range ids {
		blob, err := records.blob(ctx, gameID, id)
		if err != nil {
			return application.ImpactSnapshot{}, err
		}
		result.Blobs = append(result.Blobs, blob)
	}
	result.Counts, err = records.counts(ctx, gameID)
	if err != nil {
		return application.ImpactSnapshot{}, err
	}
	result.SourceKinds, err = dbapi.QueryStrings(ctx, records.executor, `SELECT metadata_source_kind FROM games WHERE id=?
UNION SELECT content_source_kind FROM games WHERE id=? ORDER BY 1`, gameID, gameID)
	if err != nil {
		return application.ImpactSnapshot{}, err
	}
	return result, nil
}

func (records impactRecords) blob(ctx context.Context, gameID, id string) (application.ImpactBlob, error) {
	result := application.ImpactBlob{ID: id}
	err := dbapi.QueryRowContext(
		ctx, records.executor, `SELECT size_bytes FROM blobs WHERE id=?`, id).Scan(&result.SizeBytes)
	if err != nil {
		return application.ImpactBlob{}, fmt.Errorf("read impact blob size: %w", err)
	}
	result.ProtectiveReferences, err = globalReferenceCount(ctx, records.executor, id)
	if err != nil {
		return application.ImpactBlob{}, err
	}
	result.GameReferences, err = gameReferenceCount(ctx, records.executor, gameID, id)
	if err != nil {
		return application.ImpactBlob{}, err
	}
	return result, nil
}
