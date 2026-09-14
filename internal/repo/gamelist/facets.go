package gamelist

import (
	"context"
	"fmt"

	application "retrom/internal/model/gamelist"
)

func (repository *Repository) facets(
	ctx context.Context,
	filteredConditions []string,
	filteredArguments []any,
) (int64, application.Facets, error) {
	baseFrom := `
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN platforms p ON p.id=pi.platform_id
`
	var filteredCount int64
	if err := repository.database.QueryRowContext(
		ctx,
		withConditions("SELECT count(*) "+baseFrom, filteredConditions, ""),
		filteredArguments...,
	).Scan(&filteredCount); err != nil {
		return 0, application.Facets{}, fmt.Errorf("count filtered games: %w", err)
	}
	platforms, err := repository.facetRows(
		ctx,
		"SELECT p.id,p.name,count(*) "+baseFrom,
		" GROUP BY p.id,p.name ORDER BY p.name,p.id",
		false,
	)
	if err != nil {
		return 0, application.Facets{}, fmt.Errorf("list game platform facets: %w", err)
	}
	platformInstances, err := repository.facetRows(
		ctx,
		"SELECT pi.id,pi.name,p.id,count(*) "+baseFrom,
		" GROUP BY pi.id,pi.name,p.id ORDER BY pi.name,pi.id",
		true,
	)
	if err != nil {
		return 0, application.Facets{}, fmt.Errorf("list game directory facets: %w", err)
	}
	tagFrom := baseFrom + `
JOIN game_tags relation ON relation.game_id=g.id
JOIN tags tag ON tag.id=relation.tag_id AND tag.status='ACTIVE'
`
	tags, err := repository.facetRows(
		ctx,
		"SELECT tag.id,tag.name,count(*) "+tagFrom,
		" GROUP BY tag.id,tag.name ORDER BY tag.name,tag.id",
		false,
	)
	if err != nil {
		return 0, application.Facets{}, fmt.Errorf("list game tag facets: %w", err)
	}
	facets := application.Facets{
		Platforms:         platforms,
		PlatformInstances: platformInstances,
		Tags:              tags,
	}
	for _, platform := range platforms {
		facets.TotalCount += platform.Count
	}
	return filteredCount, facets, nil
}

func (repository *Repository) facetRows(
	ctx context.Context,
	query, suffix string,
	includePlatform bool,
) ([]application.Facet, error) {
	visible := []string{"g.status='PUBLISHED'", "pi.enabled=1"}
	rows, err := repository.database.QueryContext(ctx, withConditions(query, visible, suffix))
	if err != nil {
		return nil, fmt.Errorf("query game facets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]application.Facet, 0)
	for rows.Next() {
		var item application.Facet
		if includePlatform {
			err = rows.Scan(&item.ID, &item.Name, &item.PlatformID, &item.Count)
		} else {
			err = rows.Scan(&item.ID, &item.Name, &item.Count)
		}
		if err != nil {
			return nil, fmt.Errorf("scan game facet: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate game facets: %w", err)
	}
	return items, nil
}
