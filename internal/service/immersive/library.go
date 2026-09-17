package immersive

import (
	"context"

	model "retrom/internal/model/immersive"
)

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
	result, err := service.repository.LoadLibraryPage(ctx, profileID, kind, folderID, limit, cursor)
	return result, repositoryError("library", err)
}
