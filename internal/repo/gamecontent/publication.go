package gamecontent

import (
	"context"
	"fmt"

	"retrom/internal/capability/content/multidisc"
	"retrom/internal/model/gamecontent"
	"retrom/internal/repo/blobcatalog"
	"retrom/internal/repo/recordstore"
)

func (writes writes) Publish(ctx context.Context, value gamecontent.Publication) error {
	if err := writes.game(ctx, value); err != nil {
		return err
	}
	if value.Prepared.RPGMaker != nil {
		if err := writes.rpgProfile(ctx, value); err != nil {
			return err
		}
	}
	if err := writes.variant(ctx, value); err != nil {
		return err
	}
	if value.Prepared.RPGMaker != nil {
		return writes.rpgVariant(ctx, value)
	}
	if value.Prepared.ContentKind == multidisc.ContentKind {
		return writes.playlist(ctx, value)
	}
	return nil
}

func (writes writes) game(ctx context.Context, value gamecontent.Publication) error {
	snapshot, prepared := value.Snapshot, value.Prepared
	if err := requireChanged(recordstore.UpdateGames(ctx, writes.transaction, recordstore.Update{
		Set: `content_kind=?,content_source_kind='ADMIN_REPLACE',content_source_ref_id=?,
 source_manifest_json=?,source_manifest_digest=?,version=version+1,updated_at_ms=?`,
		Values: []any{prepared.ContentKind, value.JobID, string(prepared.Manifest), prepared.ManifestDigest, value.Now},
		Scope: recordstore.Scope{
			Where: `id=? AND version=? AND source_manifest_digest=? AND status='PUBLISHED'`,
			Args:  []any{snapshot.GameID, snapshot.GameVersion, snapshot.BaseManifestDigest},
		},
	})); err != nil {
		return err
	}
	if _, err := writes.transaction.ExecContext(
		ctx,
		`DELETE FROM game_files WHERE game_id=?`,
		snapshot.GameID,
	); err != nil {
		return fmt.Errorf("remove old game files: %w", err)
	}
	for _, file := range prepared.Files {
		_, err := recordstore.CreateGameFiles(
			ctx,
			writes.transaction,
			`INSERT INTO game_files(game_id,role,logical_name,blob_id,sort_order)
  VALUES(?,?,?,?,?)`,
			snapshot.GameID,
			file.Role,
			file.LogicalName,
			file.BlobID,
			file.SortOrder,
		)
		if err != nil {
			return fmt.Errorf("write replacement game file: %w", err)
		}
	}
	return nil
}

func (writes writes) variant(ctx context.Context, value gamecontent.Publication) error {
	snapshot := value.Snapshot
	if _, err := writes.transaction.ExecContext(
		ctx,
		`DELETE FROM variant_files WHERE game_variant_id=?`,
		snapshot.VariantID,
	); err != nil {
		return fmt.Errorf("remove old variant files: %w", err)
	}
	if _, err := writes.transaction.ExecContext(
		ctx,
		`DELETE FROM variant_dependencies WHERE game_variant_id=?`,
		snapshot.VariantID,
	); err != nil {
		return fmt.Errorf("remove old variant dependencies: %w", err)
	}
	return requireChanged(recordstore.UpdateGameVariants(ctx, writes.transaction, recordstore.Update{
		Set: `provider_id=?,target_id=?,dat_version_id=?,status='READY',compatibility_code='READY',
 dependency_snapshot_json=?,version=version+1,updated_at_ms=?`,
		Values: []any{
			snapshot.ProviderID,
			snapshot.TargetID,
			snapshot.DATVersionID,
			string(
				value.DependencySnapshotJSON,
			),
			value.Now,
		},
		Scope: recordstore.Scope{Where: `id=? AND game_id=?`, Args: []any{snapshot.VariantID, snapshot.GameID}},
	}))
}

func (writes writes) playlist(ctx context.Context, value gamecontent.Publication) error {
	id, err := blobcatalog.EnsureRecord(
		ctx,
		writes.transaction,
		value.Prepared.CanonicalPlaylist,
		"application/vnd.retrom.m3u",
		value.Now,
	)
	if err != nil {
		return fmt.Errorf("register replacement playlist: %w", err)
	}
	_, err = recordstore.CreateVariantFiles(
		ctx,
		writes.transaction,
		`INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
 VALUES(?,'MULTI_DISC_PLAYLIST','playlist.m3u',?,0)`,
		value.Snapshot.VariantID,
		id,
	)
	if err != nil {
		return fmt.Errorf("attach replacement playlist: %w", err)
	}
	return nil
}
