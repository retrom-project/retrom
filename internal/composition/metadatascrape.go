package composition

import (
	"time"

	dbapi "retrom/internal/database"

	"retrom/internal/filestore"
	"retrom/internal/hasheous"
	metadatapersistence "retrom/internal/persistence/metadatascrape"
	"retrom/internal/service/metadatascrape"
)

func NewMetadata(
	database dbapi.DB,
	blobs *filestore.Store,
	provider *hasheous.Provider,
	now func() time.Time,
) *metadatascrape.Service {
	repository := metadatapersistence.NewWorker(database)
	lookup := metadatascrape.NewLookup(metadatapersistence.NewCache(database), blobs, provider, now)
	recorder := metadatascrape.NewRecorder(metadatapersistence.NewRecorder(database), blobs, now)
	media := metadatascrape.NewMediaWorker(metadatapersistence.NewMedia(database), provider, blobs, now)
	processor := metadatascrape.NewProcessor(repository, lookup, recorder)
	worker := metadatascrape.NewWorker(repository, processor, now)
	return metadatascrape.NewWithMedia(metadatapersistence.NewScheduler(database), worker, media, now)
}

func NewMetadataEvidenceQueries(database dbapi.DB) *metadatascrape.EvidenceQueries {
	return metadatascrape.NewEvidenceQueries(metadatapersistence.BindEvidenceQueries(database))
}
