package libraryimport

import (
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"

	repository "retrom/internal/persistence/libraryimport"
	launchservice "retrom/internal/service/launch"
	service "retrom/internal/service/libraryimport"
)

func NewReviewPreviews(
	database dbapi.DB, provider launchservice.PreviewProvider,
	environment launchservice.PreviewEnvironment, files *filestore.Store,
) *service.ReviewPreviews {
	return service.NewReviewPreviews(
		repository.NewReviewPreviewCreation(database), provider, WithPreviewFiles(environment, files))
}
