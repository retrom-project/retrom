package composition

import (
	"database/sql"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/hasheous"
	metadatapersistence "retrom/internal/persistence/metadatascrape"
	"retrom/internal/service/metadatascrape"
)

func NewMetadata(
	database *sql.DB,
	blobs *blobstore.Store,
	provider *hasheous.Provider,
	now func() time.Time,
) *metadatascrape.Service {
	repository := metadatapersistence.NewWorker(database)
	lookup := metadatascrape.NewLookup(metadatapersistence.NewCache(database), blobs, provider, now)
	recorder := metadatascrape.NewRecorder(metadatapersistence.NewRecorder(database), blobs, now)
	assets := metadatascrape.NewAssets(metadatapersistence.NewAssets(database), provider, blobs, now)
	processor := metadatascrape.NewProcessor(repository, lookup, recorder, assets)
	worker := metadatascrape.NewWorker(repository, processor, now)
	return metadatascrape.New(metadatapersistence.NewScheduler(database), worker, now)
}

func NewMetadataEvidenceQueries(database *sql.DB) *metadatascrape.EvidenceQueries {
	return metadatascrape.NewEvidenceQueries(metadatapersistence.BindEvidenceQueries(database))
}
