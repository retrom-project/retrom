package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"

	"github.com/google/uuid"
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

func createTag(
	ctx context.Context,
	scope model.WriteScope,
	actorUserID string,
	tag normalizedCommonTag,
	now int64,
) (model.AdminItem, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.AdminItem{}, fmt.Errorf("tagging: create tag id: %w", err)
	}
	if err := scope.Changes.Insert(
		ctx, model.TagWrite{
			ID:          id.String(),
			Name:        tag.name,
			NameKey:     tag.nameKey,
			SearchText:  tag.searchText,
			ActorUserID: actorUserID,
			NowMS:       now,
		},
	); err != nil {
		return model.AdminItem{}, repositoryError("insert tag", err)
	}
	result, err := scope.Tags.Get(ctx, id.String())
	if err != nil {
		return model.AdminItem{}, repositoryError("read created tag", err)
	}
	if err := writeAudit(
		ctx,
		scope.Audit,
		actorUserID,
		"TAG_CREATED",
		"TAG",
		id.String(),
		nil,
		result,
		nil,
		now,
	); err != nil {
		return model.AdminItem{}, err
	}
	return result, nil
}

// EnsureCommonTags atomically creates missing starter tags and preserves existing administrator tags.
func (service *Service) EnsureCommonTags(ctx context.Context, actorUserID string) (model.CommonTagsResult, error) {
	if !ValidID(actorUserID) {
		return model.CommonTagsResult{}, model.ErrInvalid
	}
	definitions, err := normalizedCommonTags()
	if err != nil {
		return model.CommonTagsResult{}, err
	}
	result := model.CommonTagsResult{CreatedItems: []model.AdminItem{}, ExistingItems: []model.AdminItem{}}
	err = service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		activeByKey, err := scope.Tags.ActiveByNameKey(ctx)
		if err != nil {
			return repositoryError("ensure common tags", err)
		}
		missingCount := 0
		for _, definition := range definitions {
			if activeByKey[definition.nameKey] == "" {
				missingCount++
			}
		}
		if len(activeByKey)+missingCount > model.MaxActiveTags {
			return model.ErrLimitReached
		}
		now := service.now().UnixMilli()
		for _, definition := range definitions {
			if existingID := activeByKey[definition.nameKey]; existingID != "" {
				existing, err := scope.Tags.Get(ctx, existingID)
				if err != nil {
					return repositoryError("ensure common tags", err)
				}
				result.ExistingItems = append(result.ExistingItems, existing)
				continue
			}
			created, err := createTag(ctx, scope, actorUserID, definition, now)
			if err != nil {
				return repositoryError("ensure common tags", err)
			}
			result.CreatedItems = append(result.CreatedItems, created)
		}
		return nil
	})
	return result, repositoryError("ensure common tags", err)
}
