package home

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

var ErrInvalidRecentQuery = errors.New("INVALID_RECENT_QUERY")

const (
	RecentSortRecent   = "RECENT_DESC"
	RecentSortTitle    = "TITLE_ASC"
	RecentSortDuration = "DURATION_DESC"
	RecentSortSessions = "SESSIONS_DESC"
)

type RecentCursor struct {
	Values []string
	GameID string
}

type RecentQuery struct {
	ProfileID      string
	Query          string
	PlatformID     string
	Sort           string
	FromAtMS       *int64
	IncludeDeleted bool
	Limit          int
	Cursor         *RecentCursor
}

type RecentPage struct {
	Items      []RecentGame
	NextCursor *RecentCursor
}

type RecentStats struct {
	GameCount        int64 `json:"gameCount"`
	ActiveDurationMS int64 `json:"activeDurationMs"`
	SessionCount     int64 `json:"sessionCount"`
}

type RecentOverview struct {
	Stats         RecentStats `json:"stats"`
	Platforms     []Platform  `json:"platforms"`
	FilteredCount int64       `json:"filteredCount"`
}

func (query RecentQuery) Validate() error {
	if query.ProfileID == "" || query.Limit < 1 || query.Limit > 100 || (query.FromAtMS != nil && *query.FromAtMS < 0) {
		return ErrInvalidRecentQuery
	}
	count := 2
	switch query.Sort {
	case RecentSortRecent, RecentSortTitle:
	case RecentSortDuration, RecentSortSessions:
		count = 3
	default:
		return ErrInvalidRecentQuery
	}
	if query.Cursor == nil {
		return nil
	}
	if query.Cursor.GameID == "" || len(query.Cursor.Values) != count-1 {
		return ErrInvalidRecentQuery
	}
	if query.Sort != RecentSortTitle {
		for _, value := range query.Cursor.Values {
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed < 0 {
				return ErrInvalidRecentQuery
			}
		}
	}
	return nil
}

func (service *Service) RecentPage(ctx context.Context, query RecentQuery) (RecentPage, error) {
	if err := query.Validate(); err != nil {
		return RecentPage{}, err
	}
	fetch := query
	fetch.Limit++
	games, err := service.repository.RecentPage(ctx, fetch)
	if err != nil {
		return RecentPage{}, fmt.Errorf("read recent games: %w", err)
	}
	result := RecentPage{Items: games}
	if len(games) > query.Limit {
		result.Items = games[:query.Limit]
		last := result.Items[query.Limit-1]
		values := []string{strconv.FormatInt(last.LastPlayedAtMS, 10)}
		switch query.Sort {
		case RecentSortTitle:
			values = []string{last.Title}
		case RecentSortDuration:
			values = append([]string{strconv.FormatInt(last.ActiveDurationMS, 10)}, values...)
		case RecentSortSessions:
			values = append([]string{strconv.FormatInt(last.SessionCount, 10)}, values...)
		}
		result.NextCursor = &RecentCursor{Values: values, GameID: last.GameID}
	}
	if err := service.attachTags(ctx, recentGameIDs(result.Items), func(index int, tags []Tag) {
		result.Items[index].Tags = tags
	}); err != nil {
		return RecentPage{}, err
	}
	return result, nil
}

func (service *Service) RecentOverview(ctx context.Context, query RecentQuery) (RecentOverview, error) {
	if err := query.Validate(); err != nil {
		return RecentOverview{}, err
	}
	result, err := service.repository.RecentOverview(ctx, query)
	if err != nil {
		return RecentOverview{}, fmt.Errorf("read recent overview: %w", err)
	}
	return result, nil
}
