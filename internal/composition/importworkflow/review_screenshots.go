package importworkflow

import (
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/libraryimport"
	repository "retrom/internal/persistence/libraryimport"
	retromruntime "retrom/internal/runtime"
	libraryservice "retrom/internal/service/libraryimport"
)

func NewReviewScreenshots(
	database dbapi.DB, files *filestore.Store, now func() time.Time,
) *libraryservice.ScreenshotSaver {
	return libraryservice.NewScreenshotSaver(
		repository.NewScreenshots(database), libraryimport.NewReviewScreenshotImages(files),
		libraryservice.ScreenshotEnvironment{Now: now, Matches: retromruntime.MatchesCapability},
	)
}
