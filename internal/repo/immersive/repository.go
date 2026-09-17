package immersive

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/model/immersive"
	"retrom/internal/repo/dbexec"
)

type (
	Repository      struct{ database *sql.DB }
	platformRecords struct{ database dbexec.Executor }
	libraryRecords  struct{ database dbexec.Executor }
	saveRecords     struct{ database dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) beginRead(ctx context.Context) (immersive.ReadScope, *sql.Tx, error) {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return immersive.ReadScope{}, nil, fmt.Errorf("immersive: begin read snapshot: %w", err)
	}
	scope := immersive.ReadScope{
		Platforms: platformRecords{transaction},
		Libraries: libraryRecords{transaction},
		Saves:     saveRecords{transaction},
	}
	return scope, transaction, nil
}

func (repository *Repository) LoadPlatforms(ctx context.Context, profileID string) ([]immersive.Platform, error) {
	scope, tx, err := repository.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	platforms, err := scope.Platforms.Platforms(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("immersive: read platforms: %w", err)
	}
	games, err := scope.Platforms.Featured(ctx, profileID, "")
	if err != nil {
		return nil, fmt.Errorf("immersive: read featured games: %w", err)
	}
	immersive.AttachFeaturedGames(platforms, games)
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("immersive: commit read snapshot: %w", err)
	}
	return platforms, nil
}

func (repository *Repository) LoadGamePage(
	ctx context.Context,
	profileID, platformID string,
	limit int,
	cursor *immersive.GameCursor,
) (immersive.GamePage, error) {
	scope, tx, err := repository.beginRead(ctx)
	if err != nil {
		return immersive.GamePage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	platform, err := scope.Platforms.Platform(ctx, profileID, platformID)
	if err != nil {
		return immersive.GamePage{}, fmt.Errorf("immersive: read platform: %w", err)
	}
	platform.FeaturedGames, err = scope.Platforms.Featured(ctx, profileID, platformID)
	if err != nil {
		return immersive.GamePage{}, fmt.Errorf("immersive: read featured games: %w", err)
	}
	games, err := scope.Platforms.Games(ctx, profileID, platformID, limit, cursor)
	if err != nil {
		return immersive.GamePage{}, fmt.Errorf("immersive: read games: %w", err)
	}
	games, next := immersive.PageItems(games, limit, immersive.LibraryAll)
	if err := tx.Commit(); err != nil {
		return immersive.GamePage{}, fmt.Errorf("immersive: commit read snapshot: %w", err)
	}
	return immersive.GamePage{Platform: platform, Items: games, NextCursor: next}, nil
}

func (repository *Repository) LoadDestinations(ctx context.Context, profileID string) ([]immersive.Destination, error) {
	scope, tx, err := repository.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	destinations := make([]immersive.Destination, 0, 4)
	kinds := []string{
		immersive.LibraryAll, immersive.LibraryRecent,
		immersive.LibraryFavorites, immersive.LibrarySaves,
	}
	for _, kind := range kinds {
		dest, queryErr := scope.Libraries.Summary(ctx, profileID, kind, "")
		if queryErr != nil {
			return nil, fmt.Errorf("immersive: read destination: %w", queryErr)
		}
		dest.FeaturedGames, queryErr = scope.Libraries.Featured(ctx, profileID, kind, "")
		if queryErr != nil {
			return nil, fmt.Errorf("immersive: read destination featured: %w", queryErr)
		}
		dest.Name = immersive.LibraryName(kind)
		destinations = append(destinations, dest)
	}
	platforms, err := scope.Platforms.Platforms(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("immersive: read platforms: %w", err)
	}
	featuredGames, err := scope.Platforms.Featured(ctx, profileID, "")
	if err != nil {
		return nil, fmt.Errorf("immersive: read featured games: %w", err)
	}
	immersive.AttachFeaturedGames(platforms, featuredGames)
	for _, platform := range platforms {
		destinations = append(destinations, immersive.Destination{
			ID: platform.ID, Kind: "platform", Name: platform.Name,
			GameCount: platform.GameCount, LastPlayedAtMS: platform.LastPlayedAtMS,
			FeaturedGames: platform.FeaturedGames,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("immersive: commit read snapshot: %w", err)
	}
	return destinations, nil
}

func (repository *Repository) LoadLibraryPage(
	ctx context.Context,
	profileID, kind, folderID string,
	limit int,
	cursor *immersive.GameCursor,
) (immersive.LibraryPage, error) {
	scope, tx, err := repository.beginRead(ctx)
	if err != nil {
		return immersive.LibraryPage{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var folder *immersive.FavoriteFolder
	if folderID != "" {
		f, err := scope.Libraries.Folder(ctx, profileID, folderID)
		if err != nil {
			return immersive.LibraryPage{}, fmt.Errorf("immersive: read folder: %w", err)
		}
		folder = f
	}
	library, err := scope.Libraries.Summary(ctx, profileID, kind, folderID)
	if err != nil {
		return immersive.LibraryPage{}, fmt.Errorf("immersive: read library: %w", err)
	}
	library.FeaturedGames, err = scope.Libraries.Featured(ctx, profileID, kind, folderID)
	if err != nil {
		return immersive.LibraryPage{}, fmt.Errorf("immersive: read featured: %w", err)
	}
	folders := make([]immersive.FavoriteFolder, 0)
	if kind == immersive.LibraryFavorites && folderID == "" {
		folders, err = scope.Libraries.Folders(ctx, profileID)
		if err != nil {
			return immersive.LibraryPage{}, fmt.Errorf("immersive: read folders: %w", err)
		}
	}
	games, err := scope.Libraries.Games(ctx, profileID, kind, folderID, limit, cursor)
	if err != nil {
		return immersive.LibraryPage{}, fmt.Errorf("immersive: read games: %w", err)
	}
	games, next := immersive.PageItems(games, limit, kind)
	if err := enrichSaveStates(ctx, scope, kind, profileID, games); err != nil {
		return immersive.LibraryPage{}, err
	}
	library.Name = immersive.LibraryName(kind)
	if err := tx.Commit(); err != nil {
		return immersive.LibraryPage{}, fmt.Errorf("immersive: commit read snapshot: %w", err)
	}
	return immersive.LibraryPage{
		Library: library, Folder: folder, Folders: folders,
		Items: games, NextCursor: next,
	}, nil
}

func enrichSaveStates(
	ctx context.Context, scope immersive.ReadScope,
	kind, profileID string,
	games []immersive.Game,
) error {
	if kind != immersive.LibrarySaves || len(games) == 0 {
		return nil
	}
	ids := make([]string, len(games))
	for i, g := range games {
		ids[i] = g.ID
	}
	saves, err := scope.Saves.ForGames(ctx, profileID, ids)
	if err != nil {
		return fmt.Errorf("immersive: read saves: %w", err)
	}
	for i := range games {
		games[i].SaveStates = append(
			games[i].SaveStates, saves[games[i].ID]...,
		)
	}
	return nil
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
