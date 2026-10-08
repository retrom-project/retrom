package persistence

import (
	"context"

	"retrom/internal/model"
)

const directoryJSON = `jsonb_build_object('id',d.id,'platformId',d.platform_id,'name',d.name,
 'slug',d.slug,'description',d.description,'defaultCoreId',d.default_core_id,'enabled',d.enabled,'version',d.version,
 'coreIds',COALESCE((SELECT jsonb_agg(c.core_id ORDER BY c.sort_order,c.core_id) FROM platform_instance_core_tab c
 WHERE c.platform_instance_id=d.id),'[]'::jsonb),
 'gameCount',(SELECT count(*) FROM game_tab g WHERE g.platform_instance_id=d.id AND g.status='published'))`

func (r *Repository) Directories(ctx context.Context, admin bool) ([]model.Directory, error) {
	return listJSON[model.Directory](ctx, r, "SELECT "+directoryJSON+
		` FROM platform_instance_tab d WHERE $1 OR d.enabled ORDER BY d.name,d.id`, admin)
}

func (r *Repository) Directory(ctx context.Context, id string) (model.Directory, error) {
	return readJSON[model.Directory](ctx, r, "SELECT "+directoryJSON+" FROM platform_instance_tab d WHERE d.id=$1", id)
}

func (r *Repository) WriteDirectory(ctx context.Context,
	id string,
	input model.DirectoryInput,
	now int64,
	create bool,
) error {
	if create {
		if err := r.Execute(ctx, `INSERT INTO platform_instance_tab
 (id,platform_id,name,slug,description,default_core_id,enabled,version,created_at_ms,updated_at_ms)
 VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$8)`, id, input.PlatformID, input.Name, input.Slug, input.Description,
			input.DefaultCoreID, input.Enabled, now); err != nil {
			return err
		}
	} else if err := r.updateDirectory(ctx, id, input, now); err != nil {
		return err
	}

	if err := r.Execute(ctx, "DELETE FROM platform_instance_core_tab WHERE platform_instance_id=$1", id); err != nil {
		return err
	}
	for order, core := range input.CoreIDs {
		if err := r.Execute(ctx, `INSERT INTO platform_instance_core_tab(platform_instance_id,core_id,sort_order)
 VALUES($1,$2,$3)`, id, core, order); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) DeleteDirectory(ctx context.Context, id string, version int64) error {
	count, err := r.Count(ctx, `SELECT count(*) FROM game_tab WHERE platform_instance_id=$1
 AND status IN ('published','pending_review')`, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return model.ErrConflict
	}
	tag, err := r.db.Exec(ctx, "DELETE FROM platform_instance_tab WHERE id=$1 AND version=$2", id, version)
	if err != nil {
		return failure("delete directory", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return r.Execute(ctx, "DELETE FROM platform_instance_core_tab WHERE platform_instance_id=$1", id)
}

func (r *Repository) updateDirectory(ctx context.Context, id string, input model.DirectoryInput, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE platform_instance_tab SET platform_id=$2,name=$3,slug=$4,description=$5,
 default_core_id=$6,enabled=$7,version=version+1,updated_at_ms=$8 WHERE id=$1 AND version=$9`,
		id,
		input.PlatformID,
		input.Name,
		input.Slug,
		input.Description,
		input.DefaultCoreID,
		input.Enabled,
		now,
		input.Version)
	if err != nil {
		return failure("update directory", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}
