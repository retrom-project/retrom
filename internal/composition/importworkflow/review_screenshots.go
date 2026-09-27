package importworkflow

import (
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/libraryimport"
	repository "retrom/internal/persistence/libraryimport"
	retromruntime "retrom/internal/runtime"
	application "retrom/internal/service/libraryimport"
)

func NewReviewScreenshots(
	database dbapi.DB, files *filestore.Store, now func() time.Time,
) *application.ScreenshotSaver {
	return application.NewScreenshotSaver(
		repository.NewScreenshots(database), libraryimport.NewReviewScreenshotImages(files),
		application.ScreenshotEnvironment{Now: now, Matches: retromruntime.MatchesCapability},
	)
}
