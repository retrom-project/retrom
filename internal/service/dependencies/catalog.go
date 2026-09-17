package dependencies

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	model "retrom/internal/model/dependencies"

	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/format/arcadedat"
	"retrom/internal/foundation/cleanup"
)

func (service *Service) BootstrapCatalogs(ctx context.Context, now time.Time) error {
	bootstrap := catalogBootstrap{ctx: ctx, repository: service.repository, set: service.set, now: now}
	names := make([]string, 0, len(service.set.Versions))
	for name := range service.set.Versions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		version := service.set.Versions[name]
		for index, core := range version.Manifest.Cores {
			if core.DAT != nil {
				bootstrap.runCore(version, index)
			}
		}
	}
	return bootstrap.firstFailure
}

type catalogBootstrap struct {
	ctx          context.Context
	repository   model.Repository
	set          *dependencies.Set
	now          time.Time
	firstFailure error
}

func (bootstrap *catalogBootstrap) runCore(version *dependencies.Version, index int) {
	core := version.Manifest.Cores[index]
	expected := model.CatalogStats{
		MachineCount: core.ParseStats.MachineCount, ROMEntryCount: core.ParseStats.ROMEntryCount,
		DiskEntryCount: core.ParseStats.DiskEntryCount, BIOSSetCount: core.ParseStats.BIOSSetCount,
		DefaultBIOSSetCount:       core.ParseStats.DefaultBIOSSetCount,
		ExplicitBIOSMachineCount:  core.ParseStats.ExplicitBIOSMachineCount,
		BaseDependencyTargetCount: core.ParseStats.BaseDependencyTargetCount,
		UnresolvedCloneofCount:    core.ParseStats.UnresolvedCloneofCount,
		UnresolvedRomofCount:      core.ParseStats.UnresolvedRomofCount,
	}
	target, err := bootstrap.targetForCore(core.CoreID)
	if err != nil {
		bootstrap.fail(err)
		return
	}
	state, err := bootstrap.repository.FindDAT(
		bootstrap.ctx,
		model.DATLookup{
			CoreID: core.CoreID,
			Target: target,
			SHA256: core.DAT.SHA256,
		},
	)
	if err != nil {
		bootstrap.fail(fmt.Errorf("find built-in DAT index: %w", err))
		return
	}
	if state.ParseStatus == "READY" && state.Indexed == expected.MachineCount {
		bootstrap.activateReady(state.ID)
		return
	}
	if state.ParseStatus == "FAILED" {
		bootstrap.fail(fmt.Errorf("%w: built-in DAT has retained failure evidence", dependencies.ErrInvalid))
		return
	}
	jobID, err := ensureBuiltInDATJob(
		bootstrap.ctx,
		bootstrap.repository,
		state.ID,
		core.DAT.SHA256,
		"retrom-dat-v1",
		bootstrap.now,
	)
	if err != nil {
		bootstrap.fail(err)
		return
	}
	if err := claimBuiltInDATJob(bootstrap.ctx, bootstrap.repository, state.ID, jobID, bootstrap.now); err != nil {
		bootstrap.fail(err)
		return
	}
	catalog, err := bootstrap.loadCatalog(
		version,
		core.CoreID,
		core.DAT.LocalPath,
		expected,
		state.Indexed,
		state.ID,
		jobID,
	)
	if err != nil {
		bootstrap.fail(err)
		return
	}
	if err := publishBuiltInDATCatalog(
		bootstrap.ctx,
		bootstrap.repository,
		state.ID,
		jobID,
		state.Indexed,
		expected.MachineCount,
		catalog,
		bootstrap.now,
	); err != nil {
		failure := failBuiltInDAT(
			bootstrap.ctx,
			bootstrap.repository,
			state.ID,
			jobID,
			"DEPENDENCY_DAT_INDEX_WRITE_FAILED",
			bootstrap.now,
		)
		bootstrap.fail(errors.Join(err, failure))
	}
}

func (bootstrap *catalogBootstrap) targetForCore(coreID string) (model.RuntimeTarget, error) {
	target, err := targetForCore(bootstrap.set.RuntimeCatalog, coreID)
	if err != nil {
		return model.RuntimeTarget{}, err
	}
	exists, err := bootstrap.repository.TargetExists(bootstrap.ctx, target)
	if err != nil {
		return model.RuntimeTarget{}, fmt.Errorf("read selected runtime target: %w", err)
	}
	if !exists {
		return model.RuntimeTarget{}, fmt.Errorf("%w: selected runtime target missing", dependencies.ErrInvalid)
	}
	return target, nil
}

func (bootstrap *catalogBootstrap) activateReady(datID string) {
	err := bootstrap.repository.CommitWrite(bootstrap.ctx, func(scope model.WriteScope) error {
		return activateBuiltInDAT(bootstrap.ctx, scope, datID, bootstrap.now)
	})
	if err != nil {
		bootstrap.fail(fmt.Errorf("activate ready built-in DAT: %w", err))
	}
}

func (bootstrap *catalogBootstrap) loadCatalog(
	version *dependencies.Version, coreID, relativePath string, expected model.CatalogStats, indexed int64, datID, jobID string,
) (arcadedat.Catalog, error) {
	if indexed == expected.MachineCount {
		return catalogFromStats(expected), nil
	}
	file, err := os.Open(filepath.Join(version.DATRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		failure := failBuiltInDAT(
			bootstrap.ctx,
			bootstrap.repository,
			datID,
			jobID,
			"DEPENDENCY_DAT_BLOB_UNAVAILABLE",
			bootstrap.now,
		)
		return arcadedat.Catalog{}, errors.Join(fmt.Errorf("open built-in DAT: %w", err), failure)
	}
	catalog, parseErr := arcadedat.ParseCatalog(bootstrap.ctx, file, coreID)
	cleanup.Error("close", file.Close())
	if parseErr == nil && statsMatch(catalog.Stats, expected) {
		return catalog, nil
	}
	code := "DEPENDENCY_DAT_STATISTICS_MISMATCH"
	resultErr := fmt.Errorf("%w: built-in DAT statistics mismatch", dependencies.ErrInvalid)
	if parseErr != nil {
		code = "DEPENDENCY_DAT_PARSE_FAILED"
		resultErr = fmt.Errorf("parse built-in DAT: %w", parseErr)
	}
	failure := failBuiltInDAT(bootstrap.ctx, bootstrap.repository, datID, jobID, code, bootstrap.now)
	return arcadedat.Catalog{}, errors.Join(resultErr, failure)
}

func (bootstrap *catalogBootstrap) fail(err error) {
	if bootstrap.firstFailure == nil {
		bootstrap.firstFailure = err
	}
}
