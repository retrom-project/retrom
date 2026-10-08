package persistence

import (
	"context"

	"retrom/internal/model"
)

func (r *Repository) Bios(ctx context.Context, key string) (model.BiosFile, error) {
	var result model.BiosFile
	err := r.db.QueryRow(ctx, `SELECT id,requirement_key,original_filename,storage_key,size_bytes,sha256
 FROM bios_file_tab WHERE requirement_key=$1 AND status='active'`,
		key).Scan(&result.ID,
		&result.RequirementKey,
		&result.Filename,
		&result.StorageKey,
		&result.SizeBytes,
		&result.SHA256)
	return result, failure("read BIOS", err)
}

func (r *Repository) WriteBios(ctx context.Context, file model.BiosFile, now int64) error {
	if err := r.Execute(ctx,
		`UPDATE bios_file_tab SET status='deleted',
updated_at_ms=$2 WHERE requirement_key=$1 AND status='active'`,
		file.RequirementKey,
		now); err != nil {
		return err
	}
	return r.Execute(ctx,
		`INSERT INTO bios_file_tab(id,
requirement_key,
original_filename,
storage_key,
size_bytes,
sha256,
status,
created_at_ms,
updated_at_ms)
 VALUES($1,
$2,
$3,
$4,
$5,
$6,
'active',
$7,
$7)`,
		file.ID,
		file.RequirementKey,
		file.Filename,
		file.StorageKey,
		file.SizeBytes,
		file.SHA256,
		now)
}

func (r *Repository) DeleteBios(ctx context.Context, key string, now int64) error {
	return r.Execute(ctx,
		`UPDATE bios_file_tab SET status='deleted',
updated_at_ms=$2 WHERE requirement_key=$1 AND status='active'`,
		key,
		now)
}

func (r *Repository) BiosFiles(ctx context.Context) ([]model.BiosFile, error) {
	return listJSON[model.BiosFile](ctx, r,
		`SELECT jsonb_build_object('id',id,'requirementKey',requirement_key,'filename',original_filename,
 'storageKey',storage_key,'sizeBytes',size_bytes,'sha256',sha256)
 FROM bios_file_tab WHERE status='active' ORDER BY requirement_key`)
}

func (r *Repository) InstallBios(ctx context.Context, file model.BiosFile, now int64) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`INSERT INTO bios_file_tab(id,requirement_key,original_filename,storage_key,size_bytes,sha256,status,
 created_at_ms,updated_at_ms)
 VALUES($1,$2,$3,$4,$5,$6,'active',$7,$7) ON CONFLICT(requirement_key) WHERE status='active' DO NOTHING`,
		file.ID, file.RequirementKey, file.Filename, file.StorageKey, file.SizeBytes, file.SHA256, now)
	if err != nil {
		return false, failure("install scanned BIOS", err)
	}
	return tag.RowsAffected() == 1, nil
}
