package composition

import (
	"database/sql"
	"time"

	rpgmakerdetector "retrom/internal/adapter/engine/rpgmaker/detector"
	"retrom/internal/adapter/files/blobstore"
	gamecontentmodel "retrom/internal/model/gamecontent"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	gamecontentrepository "retrom/internal/repo/gamecontent"
	gamecontentservice "retrom/internal/service/gamecontent"
)

// NewGameContent assembles replacement persistence, resource detection, and cleanup.
func NewGameContent(
	database *sql.DB, blobs *blobstore.Store,
	signal gamecontentmodel.ReleaseSignal, gc payloadreleasemodel.GCStager,
	now func() time.Time, multiDiscEnabled bool,
) *gamecontentservice.Service {
	return gamecontentservice.New(gamecontentrepository.New(database), now).
		WithBlobStore(blobs).WithRPGMakerDetector(rpgmakerdetector.NewBlobDetector(blobs)).
		WithPayloadRelease(signal).WithGCStager(gc).WithMultiDiscImportEnabled(multiDiscEnabled)
}
