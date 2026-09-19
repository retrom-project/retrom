package pegasusimport

import (
	"context"
	"database/sql"
	"sync"
	"time"

	blobmodel "retrom/internal/model/blob"
	pegasusimportmodel "retrom/internal/model/pegasusimport"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/integration/libraryimport"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	repository "retrom/internal/repo/pegasusimport"
	tagpersistence "retrom/internal/repo/tagging"
	application "retrom/internal/service/pegasusimport"
	"retrom/internal/service/tagging"
)

type Service struct {
	sourceReader func(context.Context) (func(), error)
	database     *sql.DB
	blobs        *blobstore.Store
	importer     *libraryimport.Service
	roots        map[string]Root
	now          func() time.Time
	tags         *tagging.Service
	workerOnce   sync.Once
	worker       *application.Worker
}

type work = pegasusimportmodel.Work

func New(database *sql.DB, blobs *blobstore.Store, importer *libraryimport.Service, credentials *retromruntime.Credentials, configured []serversource.Root, now func() time.Time) *Service {
	sources := NewSources(blobs, credentials, configured)
	return &Service{database: database, blobs: blobs, importer: importer, roots: sources.roots, now: now, tags: tagging.New(tagpersistence.New(database), now)}
}

func (service *Service) sources() *Sources {
	return &Sources{blobs: service.blobs, roots: service.roots, sourceReader: service.sourceReader}
}
func (service *Service) Start()  { service.backgroundWorker().Start() }
func (service *Service) Close()  { service.backgroundWorker().Close() }
func (service *Service) signal() { service.backgroundWorker().Signal() }
func (service *Service) execute(ctx context.Context, unit work) {
	service.backgroundWorker().Run(ctx, unit)
}

func (service *Service) claim(ctx context.Context) (work, bool, error) {
	return application.NewLeases(repository.NewLeases(service.database), service.now).Claim(ctx)
}

func (service *Service) scan(ctx context.Context, root Root, path string) (scanResult, error) {
	return application.NewScanner(scanSource{root: root, selectedPath: path, acquire: service.acquireSourceReader}).Scan(ctx)
}

func (service *Service) acquireSourceReader(ctx context.Context) (func(), error) {
	return service.sources().acquireSourceReader(ctx)
}

func (service *Service) sanitizeTechnicalDetail(err error) string {
	return service.sources().Sanitize(err)
}

func (service *Service) copySource(ctx context.Context, root Root, selectedPath, relativePath string, size int64, facts string) (blobmodel.PreparedBlob, error) {
	return service.sources().copySource(ctx, root, selectedPath, relativePath, size, facts)
}

func (service *Service) nextItem(ctx context.Context, unit work) (executionItem, bool, error) {
	return application.NewItemWork(repository.NewItemWork(service.database), service.now).Next(ctx, unit.Identity())
}
