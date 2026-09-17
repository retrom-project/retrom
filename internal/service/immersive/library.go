package immersive

import (
	"context"

	model "retrom/internal/model/immersive"
)

func resolveLibraryFolder(
	ctx context.Context,
	reader model.LibraryReader,
	profileID, kind, folderID string,
) (model.FavoriteFolder, bool, error) {
	if folderID == "" {
		return model.FavoriteFolder{}, false, nil
	}
	if kind != model.LibraryFavorites {
		return model.FavoriteFolder{}, false, model.ErrFavoriteFolderNotFound
	}
	folder, err := reader.Folder(ctx, profileID, folderID)
	if err != nil {
		return model.FavoriteFolder{}, false, repositoryError("read folder", err)
	}
	return *folder, true, nil
}

func libraryPageItems(games []model.Game, limit int, kind string) ([]model.Game, *model.GameCursor) {
	if len(games) <= limit {
		return games, nil
	}
	last := games[limit-1]
	next := &model.GameCursor{
		TitleInitial: last.TitleInitial,
		Title:        last.Title,
		ID:           last.ID,
	}
	if kind == model.LibraryRecent {
		next.LastPlayedAtMS = last.LastPlayedAtMS
	}
	return games[:limit], next
}

func (service *Service) LibraryGames(
	ctx context.Context,
	profileID, kind, folderID string,
	limit int,
	cursor *model.GameCursor,
) (model.LibraryPage, error) {
	if !model.ValidLibraryKind(kind) {
		return model.LibraryPage{}, model.ErrLibraryNotFound
	}
	if folderID != "" && kind != model.LibraryFavorites {
		return model.LibraryPage{}, model.ErrFavoriteFolderNotFound
	}
	var result model.LibraryPage
	err := service.repository.WithRead(ctx, func(scope model.ReadScope) error {
		var err error
		result, err = readLibraryPage(ctx, scope, profileID, kind, folderID, limit, cursor)
		return err
	})
	return result, repositoryError("library", err)
}

func readLibraryPage(
	ctx context.Context,
	scope model.ReadScope,
	profileID, kind, folderID string,
	limit int,
	cursor *model.GameCursor,
) (model.LibraryPage, error) {
	resolvedFolder, hasFolder, err := resolveLibraryFolder(ctx, scope.Libraries, profileID, kind, folderID)
	if err != nil {
		return model.LibraryPage{}, repositoryError("library page", err)
	}
	var folder *model.FavoriteFolder
	if hasFolder {
		folder = &resolvedFolder
	}
	library, err := scope.Libraries.Summary(ctx, profileID, kind, folderID)
	if err != nil {
		return model.LibraryPage{}, repositoryError("library page", err)
	}
	library.FeaturedGames, err = scope.Libraries.Featured(ctx, profileID, kind, folderID)
	if err != nil {
		return model.LibraryPage{}, repositoryError("library page", err)
	}
	folders := make([]model.FavoriteFolder, 0)
	if kind == model.LibraryFavorites && folderID == "" {
		folders, err = scope.Libraries.Folders(ctx, profileID)
		if err != nil {
			return model.LibraryPage{}, repositoryError("library page", err)
		}
	}
	games, err := scope.Libraries.Games(ctx, profileID, kind, folderID, limit, cursor)
	if err != nil {
		return model.LibraryPage{}, repositoryError("library page", err)
	}
	games, next := libraryPageItems(games, limit, kind)
	if kind == model.LibrarySaves {
		if err := attachSaveStates(ctx, scope.Saves, profileID, games); err != nil {
			return model.LibraryPage{}, repositoryError("library page", err)
		}
	}
	library.Name = libraryName(kind)
	return model.LibraryPage{Library: library, Folder: folder, Folders: folders, Items: games, NextCursor: next}, nil
}

func attachSaveStates(ctx context.Context, reader model.SaveReader, profileID string, games []model.Game) error {
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
