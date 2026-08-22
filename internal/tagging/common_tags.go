package tagging

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
)

var commonTagNames = [...]string{
	"动作冒险",
	"飞行射击",
	"格斗对战",
	"角色扮演",
	"模拟经营",
	"即时战略",
	"体育竞技",
	"益智解谜",
	"光枪射击",
	"生存恐怖",
}

type normalizedCommonTag struct {
	name       string
	nameKey    string
	searchText string
}

// CommonTagNames returns the administrator-editable starter taxonomy in its stable display order.
func CommonTagNames() []string {
	result := make([]string, len(commonTagNames))
	copy(result, commonTagNames[:])
	return result
}

func normalizedCommonTags() ([]normalizedCommonTag, error) {
	result := make([]normalizedCommonTag, 0, len(commonTagNames))
	for _, rawName := range commonTagNames {
		name, nameKey, searchText, err := NormalizeName(rawName)
		if err != nil {
			return nil, fmt.Errorf("tagging: normalize common tag %q: %w", rawName, err)
		}
		result = append(result, normalizedCommonTag{name: name, nameKey: nameKey, searchText: searchText})
	}
	return result, nil
}

func activeTagIDsByNameKey(ctx context.Context, database executor) (map[string]string, error) {
	rows, err := database.QueryContext(ctx, `SELECT id,name_key FROM tags WHERE status='ACTIVE'`)
	if err != nil {
		return nil, fmt.Errorf("tagging: list active tag keys: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make(map[string]string)
	for rows.Next() {
		var id, nameKey string
		if err := rows.Scan(&id, &nameKey); err != nil {
			return nil, fmt.Errorf("tagging: scan active tag key: %w", err)
		}
		result[nameKey] = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tagging: iterate active tag keys: %w", err)
	}
	return result, nil
}

func createCommonTag(
	ctx context.Context,
	database executor,
	actorUserID string,
	tag normalizedCommonTag,
	now int64,
) (AdminItem, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return AdminItem{}, fmt.Errorf("tagging: create common tag id: %w", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO tags(id,name,name_key,search_text,status,version,created_by_user_id,updated_by_user_id,
created_at_ms,updated_at_ms,deleted_at_ms) VALUES(?,?,?,?,'ACTIVE',1,?,?,?,?,NULL)
`, id.String(), tag.name, tag.nameKey, tag.searchText, actorUserID, actorUserID, now, now); err != nil {
		return AdminItem{}, fmt.Errorf("tagging: create common tag: %w", err)
	}
	result, err := adminItemByID(ctx, database, id.String())
	if err != nil {
		return AdminItem{}, err
	}
	if err := writeAudit(
		ctx, database, actorUserID, "TAG_CREATED", "TAG", id.String(), nil, result, nil, now,
	); err != nil {
		return AdminItem{}, err
	}
	return result, nil
}

// EnsureCommonTags atomically creates missing starter tags and preserves existing administrator tags.
func (service *Service) EnsureCommonTags(ctx context.Context, actorUserID string) (CommonTagsResult, error) {
	if !ValidID(actorUserID) {
		return CommonTagsResult{}, ErrInvalid
	}
	definitions, err := normalizedCommonTags()
	if err != nil {
		return CommonTagsResult{}, err
	}
	result := CommonTagsResult{CreatedItems: []AdminItem{}, ExistingItems: []AdminItem{}}
	err = service.withImmediateWrite(ctx, func(connection *sql.Conn) error {
		activeByKey, err := activeTagIDsByNameKey(ctx, connection)
		if err != nil {
			return err
		}
		missingCount := 0
		for _, definition := range definitions {
			if activeByKey[definition.nameKey] == "" {
				missingCount++
			}
		}
		if len(activeByKey)+missingCount > MaxActiveTags {
			return ErrLimitReached
		}
		now := service.now().UnixMilli()
		for _, definition := range definitions {
			if existingID := activeByKey[definition.nameKey]; existingID != "" {
				existing, err := adminItemByID(ctx, connection, existingID)
				if err != nil {
					return err
				}
				result.ExistingItems = append(result.ExistingItems, existing)
				continue
			}
			created, err := createCommonTag(ctx, connection, actorUserID, definition, now)
			if err != nil {
				return err
			}
			result.CreatedItems = append(result.CreatedItems, created)
		}
		return nil
	})
	return result, err
}
