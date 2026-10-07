package persistence

import (
	"context"
	"strings"

	"retrom/internal/model"
)

func (r *Repository) CreateGame(ctx context.Context, userID string, game model.PreparedGame, now int64) error {
	input := game.Input
	if err := r.Execute(ctx, `INSERT INTO game_tab
 (id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,
 content_hash,runtime_config_json,source,status,version,created_at_ms,updated_at_ms)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'server_import','pending_review',1,$13,$13)`,
		game.ID, input.PlatformInstanceID, input.Title, initial(input.Title), input.Description, input.Developer,
		input.Publisher,
		input.Genre,
		nullablePlayers(input.Players),
		input.ReleaseYear,
		game.ContentHash,
		input.RuntimeConfig,
		now); err != nil {
		return err
	}
	if err := r.InsertFiles(ctx, game.ID, game.Files, now); err != nil {
		return err
	}
	for _, media := range game.Media {
		if err := r.InsertMedia(ctx, game.ID, media, now); err != nil {
			return err
		}
	}
	return r.AssignTags(ctx, game.ID, userID, input.TagIDs, now)
}

func (r *Repository) InsertFiles(ctx context.Context, gameID string, files []model.GameFile, now int64) error {
	for _, file := range files {
		if err := r.Execute(ctx, `INSERT INTO game_file_tab
 (id,game_id,logical_key,role,storage_key,size_bytes,sha256,status,created_at_ms,updated_at_ms)
 VALUES($1,$2,$3,$4,$5,$6,$7,'active',$8,$8)`, file.ID, gameID, file.LogicalKey, file.Role, file.StorageKey,
			file.SizeBytes, file.SHA256, now); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) InsertMedia(ctx context.Context, gameID string, media model.Media, now int64) error {
	return r.Execute(ctx, `INSERT INTO game_media_tab
 (id,game_id,kind,ordinal,storage_key,media_type,size_bytes,sha256,status,created_at_ms,updated_at_ms)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$9)`, media.ID, gameID, media.Kind, media.Ordinal, media.StorageKey,
		media.MediaType, media.SizeBytes, media.SHA256, now)
}

func (r *Repository) UpdateGame(ctx context.Context,
	id,
	userID,
	status string,
	input model.GameInput,
	now int64,
) error {
	tag, err := r.db.Exec(ctx, `UPDATE game_tab SET platform_instance_id=$2,title=$3,title_initial=$4,
 description=$5,developer=$6,publisher=$7,genre=$8,players=$9,release_year=$10,runtime_config_json=$11,
 version=version+1,updated_at_ms=$12 WHERE id=$1 AND version=$13 AND status=$14`,
		id, input.PlatformInstanceID, input.Title, initial(input.Title), input.Description, input.Developer, input.Publisher,
		input.Genre, nullablePlayers(input.Players), input.ReleaseYear, input.RuntimeConfig, now, input.Version, status)
	if err != nil {
		return failure("update game", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return r.AssignTags(ctx, id, userID, input.TagIDs, now)
}

func (r *Repository) TransitionGame(ctx context.Context, id, current, next string, version, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE game_tab SET status=$3,version=version+1,updated_at_ms=$5,
 deleted_at_ms=CASE WHEN $3='deleted' THEN $5::bigint ELSE deleted_at_ms END
 WHERE id=$1 AND status=$2 AND version=$4`, id, current, next, version, now)
	if err != nil {
		return failure("change game state", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (r *Repository) DuplicateGame(ctx context.Context, directory, hash string) (bool, error) {
	count, err := r.Count(ctx, `SELECT count(*) FROM game_tab WHERE platform_instance_id=$1 AND content_hash=$2
 AND status IN ('pending_review','published')`, directory, hash)
	return count > 0, err
}

func (r *Repository) ReplaceFiles(ctx context.Context, id string, game model.PreparedGame, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE game_tab SET content_hash=$2,runtime_config_json=$3,
 version=version+1,updated_at_ms=$4 WHERE id=$1 AND version=$5 AND status='published'`,
		id, game.ContentHash, game.Input.RuntimeConfig, now, game.Input.Version)
	if err != nil {
		return failure("replace content", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	if err = r.Execute(ctx, `UPDATE game_file_tab SET status='deleted',updated_at_ms=$2
 WHERE game_id=$1 AND status='active'`, id, now); err != nil {
		return err
	}
	return r.InsertFiles(ctx, id, game.Files, now)
}

func (r *Repository) DeleteMedia(ctx context.Context, gameID, mediaID string, now int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE game_media_tab SET status='deleted',updated_at_ms=$3
 WHERE game_id=$1 AND id=$2 AND status='active'`, gameID, mediaID, now)
	if err != nil {
		return failure("delete game media", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrNotFound
	}
	return nil
}

func initial(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 {
		return ""
	}
	return strings.ToUpper(string(runes[0]))
}

func nullablePlayers(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (r *Repository) BumpGame(ctx context.Context, id string, version, now int64) error {
	tag, err := r.db.Exec(ctx,
		"UPDATE game_tab SET version=version+1,updated_at_ms=$3 WHERE id=$1 AND version=$2", id, version, now)
	if err != nil {
		return failure("update game version", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (r *Repository) ChangeMedia(ctx context.Context, id string, media model.Media, version, now int64) error {
	if media.Kind == "screenshot" {
		ordinal, err := r.screenshotOrdinal(ctx, id)
		if err != nil {
			return err
		}
		media.Ordinal = ordinal
	} else if err := r.Execute(ctx, `UPDATE game_media_tab SET status='deleted',updated_at_ms=$3
 WHERE game_id=$1 AND kind=$2 AND status='active'`, id, media.Kind, now); err != nil {
		return err
	}
	if err := r.InsertMedia(ctx, id, media, now); err != nil {
		return err
	}
	return r.BumpGame(ctx, id, version, now)
}

func (r *Repository) screenshotOrdinal(ctx context.Context, id string) (int64, error) {
	var count, ordinal int64
	err := r.db.QueryRow(ctx, `SELECT count(*),COALESCE(max(ordinal),-1)+1 FROM game_media_tab
 WHERE game_id=$1 AND kind='screenshot' AND status='active'`, id).Scan(&count, &ordinal)
	if err != nil {
		return 0, failure("read screenshot positions", err)
	}
	if count >= 16 {
		return 0, model.ErrInvalid
	}
	return ordinal, nil
}
