package serverimport

import (
	"context"
	"os"
	"time"

	"retrom/internal/adapter/files/serversource"
)

// Worker access is test-only so integration tests can exercise crash boundaries without exporting them in the application API.
func (service *Service) ClaimForTest(ctx context.Context) (Work, bool, error) {
	return service.claim(ctx)
}

func (service *Service) ExecuteForTest(ctx context.Context, unit Work) { service.execute(ctx, unit) }

func (service *Service) LoadItemsForTest(ctx context.Context, id string) ([]CatalogItem, error) {
	return service.loadItems(ctx, id)
}

func (service *Service) LoadPersistedCandidatesForTest(ctx context.Context, id string, items []CatalogItem) (map[string][]*EvaluatedCandidate, error) {
	return service.loadPersistedCandidates(ctx, id, items)
}

func (service *Service) PersistCandidatesForTest(ctx context.Context, unit Work, groups map[string][]*EvaluatedCandidate, counts serversource.Counts) error {
	return service.persistCandidates(ctx, unit, groups, counts)
}

func (service *Service) CompleteItemForTest(ctx context.Context, unit Work, id, state string, candidate *EvaluatedCandidate, code string) {
	service.completeItem(ctx, unit, id, state, candidate, code)
}

func (service *Service) FinishTaskForTest(ctx context.Context, unit Work) {
	service.finishTask(ctx, unit)
}

func (service *Service) ProgressForTest(ctx context.Context, unit Work, phase string, current, total int64) {
	service.progress(ctx, unit, phase, current, total)
}

func (service *Service) FailTaskForTest(ctx context.Context, unit Work, code string) {
	service.failTask(ctx, unit, code)
}

func (service *Service) CommitCandidateForTest(ctx context.Context, unit Work, item CatalogItem, candidate *EvaluatedCandidate) {
	service.commitCandidate(ctx, unit, item, candidate)
}

func (service *Service) ExecuteDiscoveryForTest(ctx context.Context, unit Work, directory *os.File, items []CatalogItem) (map[string][]*EvaluatedCandidate, bool) {
	return service.executeDiscovery(ctx, unit, directory, items)
}

func (service *Service) VerifySelectedForTest(ctx context.Context, unit Work, root Root, selected *EvaluatedCandidate) (*EvaluatedCandidate, error) {
	return service.verifySelected(ctx, unit, root, selected)
}
func (service *Service) RootForTest(id string) Root         { return service.roots[id] }
func (service *Service) RootDigestForTest(id string) string { return service.roots[id].digest }
func (service *Service) SourceSelectorForTest() SourceSelector {
	return configuredSources{service.roots}
}
func (service *Service) NowForTest() time.Time           { return service.now() }
func (service *Service) SetFileLimitForTest(limit int64) { service.scanLimits.maxFiles = limit }
func (service *Service) ResetScanLimitsForTest()         { service.scanLimits = defaultScanLimits() }

func WalkFilesForTest(root *os.File, visit func(serversource.File) error) (serversource.Counts, error) {
	return walkFiles(context.Background(), root, defaultScanLimits(), visit)
}
