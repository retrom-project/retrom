package immersive

import (
	"context"
)

func resolveLibraryFolder(
	ctx context.Context,
	reader LibraryReader,
	profileID, kind, folderID string,
) (FavoriteFolder, bool, error) {
	if folderID == "" {
		return FavoriteFolder{}, false, nil
	}
	if kind != LibraryFavorites {
		return FavoriteFolder{}, false, ErrFavoriteFolderNotFound
	}
	folder, err := reader.Folder(ctx, profileID, folderID)
	if err != nil {
		return FavoriteFolder{}, false, repositoryError("read folder", err)
	}
	return *folder, true, nil
}

func libraryPageItems(games []Game, limit int, kind string) ([]Game, *GameCursor) {
	if len(games) <= limit {
		return games, nil
	}
	last := games[limit-1]
	next := &GameCursor{
		TitleInitial: last.TitleInitial,
		Title:        last.Title,
		ID:           last.ID,
	}
	if kind == LibraryRecent {
		next.LastPlayedAtMS = last.LastPlayedAtMS
	}
	return games[:limit], next
}

func (service *Service) LibraryGames(
	ctx context.Context,
	profileID, kind, folderID string,
	limit int,
	cursor *GameCursor,
) (LibraryPage, error) {
	if !ValidLibraryKind(kind) {
		return LibraryPage{}, ErrLibraryNotFound
	}
	if folderID != "" && kind != LibraryFavorites {
		return LibraryPage{}, ErrFavoriteFolderNotFound
	}
	var result LibraryPage
	err := service.repository.WithRead(ctx, func(scope ReadScope) error {
		var err error
		result, err = readLibraryPage(ctx, scope, profileID, kind, folderID, limit, cursor)
		return err
	})
	return result, repositoryError("library", err)
}

func readLibraryPage(
	ctx context.Context,
	scope ReadScope,
	profileID, kind, folderID string,
	limit int,
	cursor *GameCursor,
) (LibraryPage, error) {
	resolvedFolder, hasFolder, err := resolveLibraryFolder(ctx, scope.Libraries, profileID, kind, folderID)
	if err != nil {
		return LibraryPage{}, repositoryError("library page", err)
	}
	var folder *FavoriteFolder
	if hasFolder {
		folder = &resolvedFolder
	}
	library, err := scope.Libraries.Summary(ctx, profileID, kind, folderID)
	if err != nil {
		return LibraryPage{}, repositoryError("library page", err)
	}
	library.FeaturedGames, err = scope.Libraries.Featured(ctx, profileID, kind, folderID)
	if err != nil {
		return LibraryPage{}, repositoryError("library page", err)
	}
	folders := make([]FavoriteFolder, 0)
	if kind == LibraryFavorites && folderID == "" {
		folders, err = scope.Libraries.Folders(ctx, profileID)
		if err != nil {
			return LibraryPage{}, repositoryError("library page", err)
		}
	}
	games, err := scope.Libraries.Games(ctx, profileID, kind, folderID, limit, cursor)
	if err != nil {
		return LibraryPage{}, repositoryError("library page", err)
	}
	games, next := libraryPageItems(games, limit, kind)
	if kind == LibrarySaves {
		if err := attachSaveStates(ctx, scope.Saves, profileID, games); err != nil {
			return LibraryPage{}, repositoryError("library page", err)
		}
	}
	library.Name = libraryName(kind)
	return LibraryPage{Library: library, Folder: folder, Folders: folders, Items: games, NextCursor: next}, nil
}

func attachSaveStates(ctx context.Context, reader SaveReader, profileID string, games []Game) error {
	if len(games) == 0 {
		return nil
	}
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.ID)
	}
	saves, err := reader.ForGames(ctx, profileID, ids)
	if err != nil {
		return repositoryError("read save states", err)
	}
	for index := range games {
		games[index].SaveStates = append(games[index].SaveStates, saves[games[index].ID]...)
	}
	return nil
}
