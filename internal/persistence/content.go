package persistence

import (
	"context"
	"fmt"

	"retrom/internal/model"
)

func (r *Repository) insertMembers(ctx context.Context, file model.GameFile) error {
	if err := model.ValidatePackage(file); err != nil {
		return failure("validate package", err)
	}
	if file.Role != "project_package" {
		return nil
	}
	for start := 0; start < len(file.Members); start += 1000 {
		end := min(start+1000, len(file.Members))
		paths, hashes, types := []string{}, []string{}, []string{}
		offsets, sizes := []int64{}, []int64{}
		for _, member := range file.Members[start:end] {
			if !validMemberInterval(member, file.SizeBytes) {
				return model.ErrInvalid
			}
			paths = append(paths, member.LogicalPath)
			hashes = append(hashes, member.SHA256)
			types = append(types, member.MediaType)
			offsets = append(offsets, member.DataOffset)
			sizes = append(sizes, member.SizeBytes)
		}
		if err := r.Execute(ctx, `INSERT INTO game_file_member_tab
 (game_file_id,logical_path,data_offset,size_bytes,sha256,media_type)
 SELECT $1,m.* FROM unnest($2::text[],$3::bigint[],$4::bigint[],$5::text[],$6::text[]) m`,
			file.ID, paths, offsets, sizes, hashes, types); err != nil {
			return err
		}
	}
	return nil
}

// PackageMembers provides bounded keyset pages; equality uses the same composite PK.
func (r *Repository) PackageMembers(ctx context.Context, id, after string, limit int) ([]model.GameFileMember, error) {
	if limit < 1 || limit > 1000 {
		return nil, model.ErrInvalid
	}
	rows, err := r.db.Query(ctx, `SELECT logical_path,data_offset,size_bytes,sha256,media_type FROM game_file_member_tab
 WHERE game_file_id=$1 AND logical_path>$2 COLLATE "C" ORDER BY logical_path LIMIT $3`, id, after, limit)
	if err != nil {
		return nil, failure("list package members", err)
	}
	defer rows.Close()
	result := make([]model.GameFileMember, 0)
	for rows.Next() {
		var m model.GameFileMember
		if err = rows.Scan(&m.LogicalPath, &m.DataOffset, &m.SizeBytes, &m.SHA256, &m.MediaType); err != nil {
			return nil, failure("read package member", err)
		}
		result = append(result, m)
	}
	return result, failure("iterate package members", rows.Err())
}

func (r *Repository) PackageMember(ctx context.Context, id, path string) (model.GameFileMember, error) {
	m := model.GameFileMember{LogicalPath: path}
	err := r.db.QueryRow(ctx, `SELECT data_offset,size_bytes,sha256,media_type FROM game_file_member_tab
 WHERE game_file_id=$1 AND logical_path=$2`, id, path).Scan(&m.DataOffset, &m.SizeBytes, &m.SHA256, &m.MediaType)
	return m, failure("read package member", err)
}

// ContentFiles expands immutable packages; ordinary game lists never call it.
func (r *Repository) ContentFiles(ctx context.Context, files []model.GameFile) ([]model.ContentFile, error) {
	for _, file := range files {
		if file.Role == "project_package" {
			if len(files) != 1 {
				return nil, fmt.Errorf("mixed project storage: %w", model.ErrInvalid)
			}
			return r.packageContent(ctx, file)
		}
	}
	return model.StandaloneFiles(files), nil
}

func (r *Repository) packageContent(ctx context.Context, file model.GameFile) ([]model.ContentFile, error) {
	cacheKey := file.ID + ":" + file.SHA256
	if cached := r.contentCache.get(cacheKey); cached != nil {
		return cached, nil
	}
	result := make([]model.ContentFile, 0)
	after := ""
	for {
		members, err := r.PackageMembers(ctx, file.ID, after, 1000)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			if !validMemberInterval(m, file.SizeBytes) {
				return nil, model.ErrInvalid
			}
			result = append(result, model.ContentFile{
				LogicalKey: m.LogicalPath, StorageKey: file.StorageKey,
				SizeBytes: m.SizeBytes, SHA256: m.SHA256, Offset: m.DataOffset, MediaType: m.MediaType, Package: &file,
			})
			after = m.LogicalPath
		}
		if len(result) > 50_000 {
			return nil, model.ErrInvalid
		}
		if len(members) < 1000 {
			break
		}
	}
	if len(result) == 0 {
		return nil, model.ErrInvalid
	}
	file.MemberCount = len(result)
	r.contentCache.put(cacheKey, result)
	return result, nil
}

func validMemberInterval(member model.GameFileMember, size int64) bool {
	return member.DataOffset >= 0 && member.SizeBytes >= 0 && member.DataOffset <= size &&
		member.SizeBytes <= size-member.DataOffset
}
