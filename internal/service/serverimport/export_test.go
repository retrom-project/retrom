package serverimport

import (
	"context"
	"os"
	"time"

	servermodel "retrom/internal/model/serverimport"

	"retrom/internal/adapter/files/serversource"
)

// Worker access is test-only so integration tests can exercise crash boundaries without exporting them in the application API.
func (service *Service) ClaimForTest(ctx context.Context) (servermodel.Work, bool, error) {
	return service.claim(ctx)
}

func (service *Service) ExecuteForTest(ctx context.Context, unit servermodel.Work) {
	service.execute(ctx, unit)
}

func (service *Service) LoadItemsForTest(ctx context.Context, id string) ([]servermodel.CatalogItem, error) {
	return service.loadItems(ctx, id)
}

func (service *Service) LoadPersistedCandidatesForTest(ctx context.Context, id string, items []servermodel.CatalogItem) (map[string][]*EvaluatedCandidate, error) {
	return service.loadPersistedCandidates(ctx, id, items)
}

func (service *Service) PersistCandidatesForTest(ctx context.Context, unit servermodel.Work, groups map[string][]*EvaluatedCandidate, counts servermodel.DiscoveryCounts) error {
	return service.persistCandidates(ctx, unit, groups, counts)
}

func (service *Service) CompleteItemForTest(ctx context.Context, unit servermodel.Work, id, state string, candidate *EvaluatedCandidate, code string) {
	service.completeItem(ctx, unit, id, state, candidate, code)
}

func (service *Service) FinishTaskForTest(ctx context.Context, unit servermodel.Work) {
	service.finishTask(ctx, unit)
}

func (service *Service) ProgressForTest(ctx context.Context, unit servermodel.Work, phase string, current, total int64) {
	service.progress(ctx, unit, phase, current, total)
}

func (service *Service) FailTaskForTest(ctx context.Context, unit servermodel.Work, code string) {
	service.failTask(ctx, unit, code)
}

func (service *Service) CommitCandidateForTest(ctx context.Context, unit servermodel.Work, item servermodel.CatalogItem, candidate *EvaluatedCandidate) {
	service.commitCandidate(ctx, unit, item, candidate)
}

func (service *Service) ExecuteDiscoveryForTest(ctx context.Context, unit servermodel.Work, directory *os.File, items []servermodel.CatalogItem) (map[string][]*EvaluatedCandidate, bool) {
	return service.executeDiscovery(ctx, unit, directory, items)
}

func (service *Service) VerifySelectedForTest(ctx context.Context, unit servermodel.Work, root Root, selected *EvaluatedCandidate) (*EvaluatedCandidate, error) {
	return service.verifySelected(ctx, unit, root, selected)
}
func (service *Service) RootForTest(id string) Root         { return service.roots[id] }
func (service *Service) RootDigestForTest(id string) string { return service.roots[id].digest }
func (service *Service) SourceSelectorForTest() servermodel.SourceSelector {
	return configuredSources{service.roots}
}
func (service *Service) NowForTest() time.Time           { return service.now() }
func (service *Service) SetFileLimitForTest(limit int64) { service.scanLimits.maxFiles = limit }
func (service *Service) ResetScanLimitsForTest()         { service.scanLimits = defaultScanLimits() }

func WalkFilesForTest(root *os.File, visit func(serversource.File) error) (servermodel.DiscoveryCounts, error) {
	return walkFiles(context.Background(), root, defaultScanLimits(), visit)
}
