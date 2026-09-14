package metadatascrape

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/model/metadatascrape"
)

func (reads scheduleReads) Import(ctx context.Context, id string) (metadatascrape.ImportSubject, error) {
	var item metadatascrape.ImportSubject
	err := reads.database.QueryRowContext(
		ctx,
		`SELECT j.platform_id FROM import_items i JOIN import_jobs j ON j.id=i.import_job_id WHERE i.id=?`,
		id,
	).Scan(
		&item.PlatformID,
	)
	if err != nil {
		return item, fmt.Errorf("query scrape import: %w", err)
	}
	return item, nil
}

func (reads scheduleReads) Review(ctx context.Context, id string) (metadatascrape.ReviewSubject, bool, error) {
	var item metadatascrape.ReviewSubject
	err := reads.database.QueryRowContext(
		ctx,
		`SELECT d.version,d.metadata_json FROM review_drafts d
 JOIN import_items i ON i.id=d.import_item_id WHERE i.id=? AND i.state='REVIEW_PENDING'`,
		id,
	).Scan(
		&item.Version,
		&item.MetadataJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, fmt.Errorf("query scrape review: %w", err)
	}
	return item, true, nil
}

func (reads scheduleReads) Game(ctx context.Context, id string) (metadatascrape.GameSubject, bool, error) {
	var item metadatascrape.GameSubject
	err := reads.database.QueryRowContext(ctx, `SELECT g.source_manifest_digest,g.version,p.platform_id FROM games g
 JOIN platform_instances p ON p.id=g.platform_instance_id WHERE g.id=? AND g.status='PUBLISHED'`, id).
		Scan(&item.ManifestDigest, &item.Version, &item.PlatformID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, fmt.Errorf("query scrape game: %w", err)
	}
	return item, true, nil
}

func (reads scheduleReads) DAT(
	ctx context.Context,
	subject metadatascrape.Subject,
) (metadatascrape.DATBinding, bool, error) {
	query := `SELECT dat_version_id,dependency_snapshot_json FROM import_item_core_validations
 WHERE import_item_id=? AND dat_version_id IS NOT NULL ORDER BY created_at_ms DESC,id DESC LIMIT 1`
	if subject.Kind == "GAME" {
		query = `SELECT v.dat_version_id,v.dependency_snapshot_json FROM games g
 JOIN platform_instances p ON p.id=g.platform_instance_id JOIN game_variants v ON v.game_id=g.id
 AND v.core_id=p.default_core_id WHERE g.id=? AND v.dat_version_id IS NOT NULL`
	}
	var binding metadatascrape.DATBinding
	err := reads.database.QueryRowContext(ctx, query, subject.ID).Scan(&binding.ID, &binding.SnapshotJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return binding, false, nil
	}
	if err != nil {
		return binding, false, fmt.Errorf("query scrape DAT binding: %w", err)
	}
	return binding, true, nil
}
