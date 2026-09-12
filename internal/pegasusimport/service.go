package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	repository "retrom/internal/persistence/pegasusimport"

	application "retrom/internal/service/pegasusimport"

	tagpersistence "retrom/internal/persistence/tagging"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
	"retrom/internal/service/tagging"
)

var (
	ErrNotFound        = application.ErrNotFound
	ErrMetadataAbsent  = application.ErrMetadataAbsent
	ErrScanLimit       = application.ErrScanLimit
	ErrMapping         = application.ErrMapping
	ErrVersionConflict = application.ErrVersionConflict
	ErrNoSelection     = application.ErrNoSelection
	ErrSourceChanged   = application.ErrSourceChanged
	ErrExpired         = application.ErrExpired
	ErrActive          = application.ErrActive
	ErrInvalid         = application.ErrInvalid
	ErrNotCancellable  = application.ErrNotCancellable
	ErrNotRetryable    = application.ErrNotRetryable
)

type Root struct {
	ID, Label string
	path      string
	digest    string
}

type CreateRequest = application.CreateRequest

type (
	RootRef            = application.RootRef
	CreatedBy          = application.CreatedBy
	Counts             = application.Counts
	Summary            = application.Summary
	Collection         = application.Collection
	Mapping            = application.Mapping
	Item               = application.Item
	FailureDetails     = application.FailureDetails
	RuntimeCheck       = application.RuntimeCheck
	RuntimeDependency  = application.RuntimeDependency
	RuntimeBIOS        = application.RuntimeBIOS
	RuntimeMissingDisc = application.RuntimeMissingDisc
	ItemMedia          = application.ItemMedia
	ExistingMatch      = application.ExistingMatch
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

func New(
	database *sql.DB,
	blobs *blobstore.Store,
	importer *libraryimport.Service,
	credentials *retromruntime.Credentials,
	configured []serversource.Root,
	now func() time.Time,
) *Service {
	roots := make(map[string]Root, len(configured))
	for _, configuredRoot := range configured {
		digest := credentials.ServerImportRootDigest(configuredRoot.ID, configuredRoot.Path)
		roots[configuredRoot.ID] = Root{
			ID:     configuredRoot.ID,
			Label:  configuredRoot.Label,
			path:   configuredRoot.Path,
			digest: hex.EncodeToString(digest[:]),
		}
	}
	return &Service{
		database: database, blobs: blobs, importer: importer, roots: roots, now: now, tags: tagging.New(
			tagpersistence.New(
				database,
			),
			now,
		),
	}
}

func (service *Service) Start() { service.backgroundWorker().Start() }

func (service *Service) Close() { service.backgroundWorker().Close() }

func (service *Service) signal() { service.backgroundWorker().Signal() }

type work = application.Work

func (service *Service) claim(ctx context.Context) (work, bool, error) {
	unit, found, err := application.NewLeases(repository.NewLeases(service.database), service.now).Claim(ctx)
	if err != nil {
		return work{}, false, fmt.Errorf("pegasusimport/claim: %w", err)
	}
	return unit, found, nil
}

func (service *Service) execute(ctx context.Context, unit work) {
	service.backgroundWorker().Run(ctx, unit)
}

func (service *Service) executeContent(ctx context.Context, unit work) {
	if ctx.Err() != nil {
		return
	}
	root, ok := service.roots[unit.RootID]
	if !ok || root.digest != unit.RootDigest {
		service.fail(ctx, unit, "SERVER_IMPORT_ROOT_CHANGED", false)
		return
	}
	if unit.Kind == "SERVER_PEGASUS_SCAN" {
		service.executeScan(ctx, unit, root)
		return
	}
	service.executeImport(ctx, unit, root)
}

func (service *Service) Create(ctx context.Context, request CreateRequest, actorID string) (Summary, error) {
	creation := application.NewCreation(
		repository.NewCreation(service.database), creationSourceSelector{service}, service.now,
	)
	result, err := creation.Create(

		ctx,

		request,

		actorID,
	)
	if err != nil {
		return Summary{}, fmt.Errorf("create Pegasus import: %w", err)
	}
	service.signal()
	return result, nil
}

type creationSourceSelector struct{ service *Service }

func (source creationSourceSelector) Select(
	ctx context.Context,
	rootID, path string,
) (application.SelectedRoot, error) {
	if err := ctx.Err(); err != nil {
		return application.SelectedRoot{}, fmt.Errorf("select Pegasus source: %w", err)
	}
	root, err := source.service.validateCreateRequest(CreateRequest{RootID: rootID, SourceRelativePath: path})
	if err != nil {
		return application.SelectedRoot{}, err
	}
	return application.SelectedRoot{ID: root.ID, Label: root.Label, Digest: root.digest}, nil
}

func (service *Service) validateCreateRequest(request CreateRequest) (Root, error) {
	if err := serversource.ValidateRootID(request.RootID); err != nil {
		return Root{}, fmt.Errorf("pegasusimport/validate root ID: %w", err)
	}
	root, ok := service.roots[request.RootID]
	if !ok {
		return Root{}, serversource.ErrRootNotFound
	}
	if err := serversource.ValidateRelativePath(request.SourceRelativePath); err != nil {
		return Root{}, fmt.Errorf("pegasusimport/validate source path: %w", err)
	}
	directory, err := serversource.OpenSelectedDirectory(root.path, request.SourceRelativePath)
	if err != nil {
		return Root{}, serversource.ErrRootUnavailable
	}
	cleanup.Error("close", directory.Close())
	return root, nil
}

func stableStrings(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func errorCode(err error) string {
	candidates := []error{
		ErrMetadataAbsent,
		ErrScanLimit,
		ErrSourceChanged,
		ErrMapping,
		ErrNoSelection,
		ErrExpired,
		ErrActive,
		ErrInvalid,
	}
	for _, candidate := range candidates {
		if errors.Is(err, candidate) {
			return candidate.Error()
		}
	}
	if err != nil && strings.HasPrefix(err.Error(), "PEGASUS_") {
		return strings.SplitN(err.Error(), ":", 2)[0]
	}
	return "INTERNAL_ERROR"
}
