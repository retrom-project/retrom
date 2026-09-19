package serverimport_test

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	servermodel "retrom/internal/model/serverimport"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/runtime/runtime"
	"retrom/internal/bootstrap/composition"
	"retrom/internal/service/firmware"
	importservice "retrom/internal/service/serverimport"
)

func New(database *sql.DB, blobs *blobstore.Store, installer *firmware.Service, credentials *runtime.Credentials, configured []serversource.Root, now func() time.Time) *importservice.Service {
	return composition.NewServerImports(database, blobs, installer, credentials, configured, now)
}

func openSelectedDirectory(path, relative string) (*os.File, error) {
	result, err := serversource.OpenSelectedDirectory(path, relative)
	if err != nil {
		return nil, fmt.Errorf("open test source directory: %w", err)
	}
	return result, nil
}

func walkFiles(directory *os.File, visit func(serversource.File) error) (servermodel.DiscoveryCounts, error) {
	result, err := importservice.WalkFilesForTest(directory, visit)
	if err != nil {
		return servermodel.DiscoveryCounts{}, fmt.Errorf("walk test source: %w", err)
	}
	return result, nil
}
