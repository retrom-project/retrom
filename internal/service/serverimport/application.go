package serverimport

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	serversourcecontract "retrom/internal/adapter/files/serversource"
	blobmodel "retrom/internal/model/blob"
	firmwaremodel "retrom/internal/model/firmware"
	model "retrom/internal/model/serverimport"

	"retrom/internal/foundation/cleanup"
)

var ErrScanLimit = errors.New("SERVER_IMPORT_SCAN_LIMIT_EXCEEDED")

type Root struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Status       string `json:"status"`
	path, digest string
}
type (
	SourceRoot struct{ ID, Label, Path, Digest string }
	BlobStore  interface {
		Put(io.Reader) (blobmodel.PreparedBlob, error)
		Path(string) string
	}
)

type FirmwareInstaller interface {
	InstallServerCandidate(
		context.Context, firmwaremodel.ServerInstallRequest,
	) (firmwaremodel.ServerInstallResult, error)
}
type Repositories struct {
	Queries   QueryRepository
	Creation  model.CreationRepository
	Control   model.ControlRepository
	Recovery  RecoveryRepository
	Discovery model.DiscoveryRepository
	Leases    model.LeaseRepository
	Outcomes  model.OutcomeRepository
}
type Options struct {
	Sources  []SourceRoot
	Blobs    BlobStore
	Firmware FirmwareInstaller
	Now      func() time.Time
}
type Service struct {
	blobs       BlobStore
	firmware    FirmwareInstaller
	roots       map[string]Root
	now         func() time.Time
	queries     *Queries
	creation    *Creation
	control     *Control
	recovery    *Recovery
	discovery   *Discovery
	leases      *Leases
	outcomes    *Outcomes
	wake        chan struct{}
	stop        chan struct{}
	stopOnce    sync.Once
	archiveScan chan struct{}
	scanLimits  scanLimits
}

func New(repositories Repositories, options Options) *Service {
	roots := make(map[string]Root, len(options.Sources))
	digests := make(map[string]string, len(options.Sources))
	for _, source := range options.Sources {
		roots[source.ID] = Root{ID: source.ID, Label: source.Label, path: source.Path, digest: source.Digest}
		digests[source.ID] = source.Digest
	}
	return &Service{
		blobs: options.Blobs, firmware: options.Firmware, roots: roots, now: options.Now,
		queries: NewQueries(
			repositories.Queries,
		), creation: NewCreation(
			repositories.Creation,
			configuredSources{
				roots,
			},
			options.Now,
		),
		control: NewControl(
			repositories.Control,
			digests,
			options.Now,
		), recovery: NewRecovery(repositories.Recovery),
		discovery: NewDiscovery(repositories.Discovery, options.Now), leases: NewLeases(repositories.Leases, options.Now),
		outcomes: NewOutcomes(
			repositories.Outcomes,
			options.Now,
		), wake: make(
			chan struct{},
			1,
		), stop: make(
			chan struct{},
		), archiveScan: make(
			chan struct{},
			1,
		), scanLimits: defaultScanLimits(),
	}
}
func (service *Service) Start() { go service.runLoop(); service.signal() }
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

func (service *Service) Directories(rootID, relativePath string) ([]serversourcecontract.Directory, error) {
	if err := ValidateRootID(rootID); err != nil {
		return nil, err
	}
	root, ok := service.roots[rootID]
	if !ok {
		return nil, serversourcecontract.ErrRootNotFound
	}
	if err := ValidateRelativePath(relativePath); err != nil {
		return nil, err
	}
	return listDirectories(root.path, relativePath)
}

func (service *Service) signal() {
	select {
	case service.wake <- struct{}{}:
	default:
	}
}

type scanLimits struct {
	maxDepth                                        int
	maxDirectories, maxFiles, maxPhysicalCandidates int64
	maxCandidatesPerRequirement                     int
	maxHashedBytes                                  int64
	hashWorkers                                     int
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
