package serverimport

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	importservice "retrom/internal/service/serverimport"

	firmwareservice "retrom/internal/service/firmware"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
)

var (
	ErrQuery          = importservice.ErrQuery
	ErrActive         = importservice.ErrActive
	ErrCatalogEmpty   = importservice.ErrCatalogEmpty
	ErrCatalogInvalid = importservice.ErrCatalogInvalid
	ErrScanLimit      = errors.New("SERVER_IMPORT_SCAN_LIMIT_EXCEEDED")
	ErrNotCancellable = importservice.ErrNotCancellable
	ErrNotRetryable   = importservice.ErrNotRetryable
	ErrNotFound       = importservice.ErrNotFound
)

type Root struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	path   string
	digest string
}

type CreateRequest = importservice.CreateRequest

type (
	Counts    = importservice.Counts
	Summary   = importservice.Summary
	RootRef   = importservice.RootRef
	CreatedBy = importservice.CreatedBy
	Item      = importservice.Item
	Candidate = importservice.Candidate
)

type Service struct {
	database    *sql.DB
	blobs       *blobstore.Store
	firmware    *firmwareservice.Service
	credentials *retromruntime.Credentials
	roots       map[string]Root
	now         func() time.Time
	wake        chan struct{}
	stop        chan struct{}
	stopOnce    sync.Once
	archiveScan chan struct{}
	scanLimits  scanLimits
}

type scanLimits struct {
	maxDepth                    int
	maxDirectories              int64
	maxFiles                    int64
	maxPhysicalCandidates       int64
	maxCandidatesPerRequirement int
	maxHashedBytes              int64
	hashWorkers                 int
}

func defaultScanLimits() scanLimits {
	return scanLimits{
		maxDepth:                    64,
		maxDirectories:              250000,
		maxFiles:                    2000000,
		maxPhysicalCandidates:       100000,
		maxCandidatesPerRequirement: 10000,
		maxHashedBytes:              2 << 40,
		hashWorkers:                 2,
	}
}

func New(
	database *sql.DB,
	blobs *blobstore.Store,
	firmwareService *firmwareservice.Service,
	credentials *retromruntime.Credentials,
	configured []serversource.Root,
	now func() time.Time,
) *Service {
	roots := make(map[string]Root, len(configured))
	for _, configuredRoot := range configured {
		digest := credentials.ServerImportRootDigest(configuredRoot.ID, configuredRoot.Path)
		roots[configuredRoot.ID] = Root{
			ID: configuredRoot.ID, Label: configuredRoot.Label, path: configuredRoot.Path,
			digest: hex.EncodeToString(digest[:]),
		}
	}
	return &Service{
		database: database, blobs: blobs, firmware: firmwareService, credentials: credentials,
		roots: roots, now: now, wake: make(chan struct{}, 1), stop: make(chan struct{}), archiveScan: make(chan struct{}, 1),
		scanLimits: defaultScanLimits(),
	}
}

func (service *Service) Start() {
	go service.runLoop()
	service.signal()
}

func (service *Service) Close() { service.stopOnce.Do(func() { close(service.stop) }) }

func (service *Service) Roots() []Root {
	result := make([]Root, 0, len(service.roots))
	for _, root := range service.roots {
		root.Status = "AVAILABLE"
		opened, err := openDirectoryNoFollow(root.path)
		if err != nil {
			root.Status = "UNAVAILABLE"
		} else {
			cleanup.Error("close", opened.Close())
		}
		root.path, root.digest = "", ""
		result = append(result, root)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}

func (service *Service) Directories(rootID, relativePath string) ([]Directory, error) {
	if err := ValidateRootID(rootID); err != nil {
		return nil, err
	}
	root, ok := service.roots[rootID]
	if !ok {
		return nil, ErrRootNotFound
	}
	if err := ValidateRelativePath(relativePath); err != nil {
		return nil, err
	}
	return listDirectories(root.path, relativePath)
}

type catalogItem = importservice.CatalogItem

func (service *Service) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}
