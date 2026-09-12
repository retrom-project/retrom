package storageanalysis

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"

	"retrom/internal/service/storageanalysis"

	"retrom/internal/cleanup"
	"retrom/internal/persistence/blobregistry"
)

type (
	Repository struct{ database *sql.DB }
	blob       struct {
		id   string
		size int64
	}
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) Read(ctx context.Context) (storageanalysis.ReadModel, error) {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return storageanalysis.ReadModel{}, fmt.Errorf("storageanalysis: begin snapshot: %w", err)
	}
	defer dbexec.Rollback(transaction)
	edges, err := blobregistry.Load()
	if err != nil {
		return storageanalysis.ReadModel{}, fmt.Errorf("storageanalysis: load references: %w", err)
	}
	if err := validateReferenceCoverage(edges); err != nil {
		return storageanalysis.ReadModel{}, err
	}
	source := storageanalysis.ReadModel{}
	source.Protected, err = blobregistry.ProtectiveSet(ctx, transaction)
	if err != nil {
		return storageanalysis.ReadModel{}, fmt.Errorf("storageanalysis: protected references: %w", err)
	}
	source.Usage, err = loadUsage(ctx, transaction, edges)
	if err != nil {
		return storageanalysis.ReadModel{}, err
	}
	source.Blobs, err = loadBlobs(ctx, transaction)
	if err != nil {
		return storageanalysis.ReadModel{}, err
	}
	source.Archives, err = loadArchiveMembers(ctx, transaction)
	if err != nil {
		return storageanalysis.ReadModel{}, err
	}
	source.Saves, err = loadSaveReferences(ctx, transaction)
	if err != nil {
		return storageanalysis.ReadModel{}, err
	}
	source.CleanupCandidates, err = referenceIDs(ctx, transaction, `SELECT DISTINCT blob_id FROM blob_gc_candidates`)
	if err != nil {
		return storageanalysis.ReadModel{}, err
	}
	if err := transaction.Commit(); err != nil {
		return storageanalysis.ReadModel{}, fmt.Errorf("storageanalysis: commit snapshot: %w", err)
	}
	return source, nil
}

func loadBlobs(ctx context.Context, transaction *sql.Tx) (map[string]int64, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT id, size_bytes FROM blobs ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("storageanalysis/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := map[string]int64{}
	for rows.Next() {
		var item blob
		if err := rows.Scan(&item.id, &item.size); err != nil {
			return nil, fmt.Errorf("storageanalysis/service: %w", err)
		}
		result[item.id] = item.size
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storageanalysis/service: %w", err)
	}
	return result, nil
}

func loadSaveReferences(ctx context.Context, transaction *sql.Tx) (storageanalysis.SaveReferences, error) {
	var result storageanalysis.SaveReferences
	if err := transaction.QueryRowContext(ctx, `
SELECT COUNT(*) FILTER (WHERE deleted_at_ms IS NULL),COUNT(*) FILTER (WHERE deleted_at_ms IS NOT NULL) FROM save_states
`).Scan(&result.ActiveCount, &result.DeletedCount); err != nil {
		return result, fmt.Errorf("storageanalysis: save counts: %w", err)
	}
	var err error
	result.PayloadIDs, err = referenceIDs(ctx, transaction, `SELECT DISTINCT payload_blob_id FROM save_states`)
	if err != nil {
		return result, err
	}
	result.ScreenshotIDs, err = referenceIDs(
		ctx,
		transaction,
		`SELECT DISTINCT screenshot_blob_id FROM save_states WHERE screenshot_blob_id IS NOT NULL`,
	)
	return result, err
}

func referenceIDs(ctx context.Context, transaction *sql.Tx, query string) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("storageanalysis: query reference IDs: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storageanalysis: scan reference ID: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storageanalysis: iterate reference IDs: %w", err)
	}
	return result, nil
}

func loadArchiveMembers(ctx context.Context, transaction *sql.Tx) ([]storageanalysis.ArchiveMember, error) {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT archive_blob_id,materialized_blob_id FROM archive_entries WHERE materialized_blob_id IS NOT NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("storageanalysis: query archive members: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]storageanalysis.ArchiveMember, 0)
	for rows.Next() {
		var member storageanalysis.ArchiveMember
		if err := rows.Scan(&member.ArchiveID, &member.MemberID); err != nil {
			return nil, fmt.Errorf("storageanalysis: scan member: %w", err)
		}
		result = append(result, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storageanalysis: iterate archive members: %w", err)
	}
	return result, nil
}
