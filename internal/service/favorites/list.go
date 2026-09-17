package favorites

import (
	"context"
	"strings"
	"unicode/utf8"

	model "retrom/internal/model/favorites"
)

func validateListOptions(options model.ListOptions) (model.ListOptions, error) {
	if options.Scope == "" {
		options.Scope = model.ScopeAll
	}
	if options.Sort == "" {
		options.Sort = model.SortFavoritedDesc
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	if options.Limit < 1 || options.Limit > 100 || utf8.RuneCountInString(strings.TrimSpace(options.Query)) > 200 {
		return model.ListOptions{}, model.ErrInvalid
	}
	options.Query = canonicalSearch(options.Query)
	switch options.Scope {
	case model.ScopeAll, model.ScopeUncategorized:
		if options.FolderID != "" {
			return model.ListOptions{}, model.ErrInvalid
		}
	case model.ScopeFolder:
		if !model.ValidID(options.FolderID) {
			return model.ListOptions{}, model.ErrInvalid
		}
	default:
		return model.ListOptions{}, model.ErrInvalid
	}
	switch options.Sort {
	case model.SortFavoritedDesc, model.SortRecentlyPlayed, model.SortTitleAsc, model.SortReleaseYearDesc:
	default:
		return model.ListOptions{}, model.ErrInvalidFavoriteListSort
	}
	return options, nil
}

func (service *Service) List(ctx context.Context, principal model.Principal, requested model.ListOptions) (model.ListResult, error) {
	options, err := validateListOptions(requested)
	if err != nil {
		return model.ListResult{}, repositoryError("List", err)
	}
	result, err := service.repository.List(ctx, principal.ProfileID, options)
	if err != nil {
		return model.ListResult{}, repositoryError("List", err)
	}
	result.GeneratedAtMS = service.now().UnixMilli()
	return result, nil
}
