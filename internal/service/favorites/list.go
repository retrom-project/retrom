package favorites

import (
	"context"
	"strings"
	"unicode/utf8"
)

func validateListOptions(options ListOptions) (ListOptions, error) {
	if options.Scope == "" {
		options.Scope = ScopeAll
	}
	if options.Sort == "" {
		options.Sort = SortFavoritedDesc
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	if options.Limit < 1 || options.Limit > 100 || utf8.RuneCountInString(strings.TrimSpace(options.Query)) > 200 {
		return ListOptions{}, ErrInvalid
	}
	options.Query = canonicalSearch(options.Query)
	switch options.Scope {
	case ScopeAll, ScopeUncategorized:
		if options.FolderID != "" {
			return ListOptions{}, ErrInvalid
		}
	case ScopeFolder:
		if !ValidID(options.FolderID) {
			return ListOptions{}, ErrInvalid
		}
	default:
		return ListOptions{}, ErrInvalid
	}
	switch options.Sort {
	case SortFavoritedDesc, SortRecentlyPlayed, SortTitleAsc, SortReleaseYearDesc:
	default:
		return ListOptions{}, ErrInvalidFavoriteListSort
	}
	return options, nil
}

func (service *Service) List(ctx context.Context, principal Principal, requested ListOptions) (ListResult, error) {
	options, err := validateListOptions(requested)
	if err != nil {
		return ListResult{}, repositoryError("List", err)
	}
	result, err := service.repository.List(ctx, principal.ProfileID, options)
	if err != nil {
		return ListResult{}, repositoryError("List", err)
	}
	result.GeneratedAtMS = service.now().UnixMilli()
	return result, nil
}
