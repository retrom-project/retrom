package composition

import (
	"database/sql"
	"log/slog"
	"time"

	archiveadapter "retrom/internal/adapter/content/archive"
	"retrom/internal/adapter/files/blobstore"
	cleanupadapter "retrom/internal/adapter/system/cleanup"
	firmwaremodel "retrom/internal/model/firmware"
	firmwarerepository "retrom/internal/repo/firmware"
	"retrom/internal/service/firmware"
)

func NewFirmware(
	database *sql.DB, now func() time.Time, blobs *blobstore.Store, releases firmwaremodel.ReleaseSignal,
) *firmware.Service {
	archives := archiveadapter.New(cleanupadapter.NewReporter(slog.Default()))
	return firmware.New(firmwarerepository.New(database), now, archives).WithBlobStore(blobs).WithPayloadRelease(releases)
}
