package composition

import (
	"database/sql"
	"encoding/hex"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/runtime/runtime"
	importpersistence "retrom/internal/persistence/serverimport"
	"retrom/internal/service/firmware"
	"retrom/internal/service/serverimport"
)

func NewServerImports(
	database *sql.DB,
	blobs *blobstore.Store,
	installer *firmware.Service,
	credentials *runtime.Credentials,
	configured []serversource.Root,
	now func() time.Time,
) *serverimport.Service {
	sources := make([]serverimport.SourceRoot, 0, len(configured))
	for _, root := range configured {
		digest := credentials.ServerImportRootDigest(root.ID, root.Path)
		sources = append(
			sources,
			serverimport.SourceRoot{
				ID:    root.ID,
				Label: root.Label,
				Path:  root.Path,
				Digest: hex.EncodeToString(
					digest[:],
				),
			},
		)
	}
	return serverimport.New(serverimport.Repositories{
		Queries: importpersistence.NewQueries(database), Creation: importpersistence.NewCreation(database),
		Control: importpersistence.NewControl(database), Recovery: importpersistence.NewRecovery(database),
		Discovery: importpersistence.NewDiscovery(
			database,
		), Leases: importpersistence.NewLeases(
			database,
		), Outcomes: importpersistence.NewOutcomes(
			database,
		),
	}, serverimport.Options{Sources: sources, Blobs: blobs, Firmware: installer, Now: now})
}
