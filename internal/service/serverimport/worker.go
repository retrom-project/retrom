package serverimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	firmwaremodel "retrom/internal/model/firmware"
	model "retrom/internal/model/serverimport"

	"retrom/internal/foundation/cleanup"
)

var (
	errCancelled = errors.New("server import cancelled")

	errSourceChanged     = errors.New("server import source changed")
	errExecutionDeadline = errors.New("server import execution deadline exceeded")
)

func (service *Service) runLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-service.stop:
			return
		case <-service.wake:
		case <-ticker.C:
		}
		for {
			handled, err := service.outcomes.Reconcile(context.Background())
			if err != nil {
				service.workerError("reconcile", err)
				break
			}
			if handled {
				continue
			}
			workUnit, ok, err := service.claim(context.Background())
			if err != nil || !ok {
				service.workerError("claim", err)
				break
			}
			service.execute(context.Background(), workUnit)
		}
	}
}

func (service *Service) claim(ctx context.Context) (model.Work, bool, error) {
	unit, found, err := service.leases.Claim(ctx)
	if err != nil {
		return model.Work{}, false, fmt.Errorf("claim server import: %w", err)
	}
	return unit, found, nil
}

// Discovery, cancellation and item commits are one state machine.
func (service *Service) execute(ctx context.Context, unit model.Work) {
	if unit.DeadlineAtMS > 0 && unit.DeadlineAtMS <= service.now().UnixMilli() {
		service.failTask(context.WithoutCancel(ctx), unit, "INTERNAL_ERROR")
		return
	}
	if unit.DeadlineAtMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, time.UnixMilli(unit.DeadlineAtMS))
		defer cancel()
	}
	heartbeatDone := make(chan struct{})
	go service.heartbeatLoop(ctx, unit, heartbeatDone)
	defer close(heartbeatDone)
	root, exists := service.roots[unit.RootID]
	if !exists || root.digest != unit.RootDigest {
		service.failTask(ctx, unit, "SERVER_IMPORT_ROOT_CHANGED")
		return
	}
	directory, err := openSelectedDirectory(root.path, unit.RelativePath)
	if err != nil {
		service.failTask(ctx, unit, "SERVER_IMPORT_ROOT_UNAVAILABLE")
		return
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	items, err := service.loadItems(ctx, unit.ImportID)
	if err != nil {
		service.failTask(ctx, unit, "INTERNAL_ERROR")
		return
	}
	byRequirement, ok := service.executeDiscovery(ctx, unit, directory, items)
	if !ok {
		return
	}
	if service.cancelRequested(ctx, unit) {
		service.cancelTask(ctx, unit)
		return
	}
	if !service.installCandidates(ctx, unit, root, items, byRequirement) {
		return
	}
	service.finishTask(ctx, unit)
}

func (service *Service) executeDiscovery(
	ctx context.Context,
	unit model.Work,
	directory *os.File,
	items []model.CatalogItem,
) (map[string][]*EvaluatedCandidate, bool) {
	resume, err := service.discoveryWasPersisted(ctx, unit.ImportID)
	if err != nil {
		service.failTask(ctx, unit, "INTERNAL_ERROR")
		return nil, false
	}
	if resume {
		candidates, err := service.loadPersistedCandidates(ctx, unit.ImportID, items)
		if err != nil {
			service.failTask(ctx, unit, "INTERNAL_ERROR")
			return nil, false
		}
		return candidates, true
	}
	if err := service.clearEvaluation(ctx, unit); err != nil {
		service.failTask(ctx, unit, "INTERNAL_ERROR")
		return nil, false
	}
	service.progress(ctx, unit, "DISCOVERING", 0, int64(len(items)))
	byRequirement, counts, err := service.discoverCandidates(ctx, unit, directory, items)
	if err != nil {
		service.failDiscovery(ctx, unit, err)
		return nil, false
	}
	for _, candidates := range byRequirement {
		markDuplicateBytes(candidates)
	}
	if err := service.persistCandidates(ctx, unit, byRequirement, counts); err != nil {
		service.failTask(ctx, unit, "INTERNAL_ERROR")
		return nil, false
	}
	service.progress(ctx, unit, "DISCOVERY_COMPLETED", int64(len(items)), int64(len(items)))
	return byRequirement, true
}

func (service *Service) failDiscovery(ctx context.Context, unit model.Work, err error) {
	switch {
	case errors.Is(err, errCancelled):
		service.cancelTask(ctx, unit)
	case errors.Is(err, ErrScanLimit):
		service.failTask(ctx, unit, "SERVER_IMPORT_SCAN_LIMIT_EXCEEDED")
	case errors.Is(err, errExecutionDeadline):
		service.failTask(context.WithoutCancel(ctx), unit, "INTERNAL_ERROR")
	default:
		service.failTask(ctx, unit, "SERVER_IMPORT_ROOT_UNAVAILABLE")
	}
}

func (service *Service) installCandidates(
	ctx context.Context,
	unit model.Work,
	root Root,
	items []model.CatalogItem,
	byRequirement map[string][]*EvaluatedCandidate,
) bool {
	service.progress(ctx, unit, "INSTALLING", 0, int64(len(items)))
	for index, item := range items {
		if item.State != "PENDING" && item.State != "EVALUATING" {
			service.progress(ctx, unit, "INSTALLING", int64(index+1), int64(len(items)))
			continue
		}
		if service.cancelRequested(ctx, unit) {
			service.cancelTask(ctx, unit)
			return false
		}
		if !service.installCandidate(ctx, unit, root, item, byRequirement[item.RequirementID]) {
			return false
		}
		service.progress(ctx, unit, "INSTALLING", int64(index+1), int64(len(items)))
	}
	return true
}

func (service *Service) installCandidate(
	ctx context.Context,
	unit model.Work,
	root Root,
	item model.CatalogItem,
	candidates []*EvaluatedCandidate,
) bool {
	if len(candidates) == 0 {
		service.completeItem(ctx, unit, item.RequirementID, "NOT_FOUND", nil, "BIOS_CANDIDATE_NOT_FOUND")
		return true
	}
	eligible := RankCandidates(candidates)
	if len(eligible) == 0 {
		state, code := rejectedArchiveOutcome(candidates)
		service.completeItem(ctx, unit, item.RequirementID, state, nil, code)
		return true
	}
	selected, err := service.verifySelected(ctx, unit, root, eligible[0])
	if errors.Is(err, errCancelled) {
		service.cancelTask(ctx, unit)
		return false
	}
	if errors.Is(err, errExecutionDeadline) {
		service.failTask(context.WithoutCancel(ctx), unit, "INTERNAL_ERROR")
		return false
	}
	if err != nil {
		service.completeItem(ctx, unit, item.RequirementID, "SOURCE_CHANGED", eligible[0],
			"SERVER_IMPORT_SOURCE_CHANGED")
		return true
	}
	service.commitCandidate(ctx, unit, item, selected)
	return true
}

func (service *Service) commitCandidate(
	ctx context.Context,
	unit model.Work,
	item model.CatalogItem,
	selected *EvaluatedCandidate,
) {
	status, method := SelectedStatus(selected)
	_, err := service.firmware.InstallServerCandidate(ctx, firmwaremodel.ServerInstallRequest{
		ServerImportID: unit.ImportID, JobID: unit.JobID, WorkerID: unit.Owner, ExecutionNo: unit.Execution,
		CandidateID: selected.ID, RequirementID: item.RequirementID, RequirementVersion: item.RequirementVersion,
		ProviderID: item.ProviderID, TargetID: item.TargetID,
		SourceVersion: item.SourceVersion, ArchiveMembersJSON: item.ArchiveMembersJSON,
		CatalogDigest: item.CatalogDigest, SourceKind: item.SourceKind, LogicalName: item.LogicalName,
		OriginalFilename: selected.File.Basename,
		Metadata:         selected.Metadata, Status: status, MatchMethod: method, Details: selected.Details,
		ArchiveEntries: selected.ArchiveEntries, ReplaceIfBetter: unit.ReplaceIfBetter,
		StaticExpectation: staticExpectation(item), StaticEvaluation: selected.Static,
		DATExpectedEntries: selected.ExpectedDATEntries, DATEvaluation: selected.DAT,
	})
	switch {
	case errors.Is(err, firmwaremodel.ErrCatalogChanged):
		service.completeItem(ctx, unit, item.RequirementID, "CATALOG_CHANGED", selected,
			"BIOS_REQUIREMENT_CATALOG_CHANGED")
	case err != nil:
		service.completeItem(ctx, unit, item.RequirementID, "COMMIT_FAILED", selected, "INTERNAL_ERROR")
	}
}

func (service *Service) heartbeatLoop(ctx context.Context, unit model.Work, done <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-service.stop:
			return
		case <-ticker.C:
			if err := service.leases.Heartbeat(ctx, unit); err != nil {
				service.workerError("heartbeat", err)
				return
			}
		}
	}
}
