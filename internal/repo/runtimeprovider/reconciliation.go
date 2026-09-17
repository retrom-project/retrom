package runtimeprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"retrom/internal/capability/runtime/runtimebundle"
	service "retrom/internal/model/runtimeprovider"

	"github.com/google/uuid"
	"golang.org/x/mod/semver"
)

func prepareReconciliation(
	ctx context.Context,
	records catalogRecords,
	current service.CurrentState,
	candidate service.Projection,
	now int64,
) (service.Publication, []string, error) {
	result := service.Publication{Candidate: candidate, AtMS: now}
	providers := make(map[string]bool)
	targets := make(map[service.TargetIdentity]bool)
	changed := make([]string, 0, len(candidate.Providers))
	for _, provider := range candidate.Providers {
		id := provider.Active.ProviderID
		providers[id] = true
		previous, exists := current.Providers[id]
		updated, err := validateProviderVersion(provider.Active, previous, exists)
		if err != nil {
			return service.Publication{}, nil, err
		}
		if updated {
			changed = append(changed, id)
		}
		for _, target := range provider.Targets {
			identity := service.TargetIdentity{ProviderID: id, TargetID: target.Target.ID}
			targets[identity] = true
			formats, err := records.CheckpointFormats(ctx, identity)
			if err != nil {
				return service.Publication{}, nil, fmt.Errorf("read stored checkpoint formats: %w", err)
			}
			if err := validateCheckpointFormats(id, target, formats); err != nil {
				return service.Publication{}, nil, err
			}
		}
	}
	for id := range current.Providers {
		if !providers[id] {
			result.RemovedProviders = append(result.RemovedProviders, id)
			changed = append(changed, id)
		}
	}
	for _, target := range current.Targets {
		if targets[target] {
			continue
		}
		referenced, err := records.TargetReferenced(ctx, target)
		if err != nil {
			return service.Publication{}, nil, fmt.Errorf("inspect removed runtime target: %w", err)
		}
		if referenced {
			return service.Publication{}, nil, fmt.Errorf(
				"%w: %s/%s", service.ErrProviderTargetReferenced,
				target.ProviderID, target.TargetID,
			)
		}
		result.RemovedTargets = append(result.RemovedTargets, target)
	}
	sort.Strings(changed)
	sort.Strings(result.RemovedProviders)
	return result, changed, nil
}

func applyChangedProjection(
	ctx context.Context,
	records projectionRecords,
	change service.Publication,
	changed []string,
) error {
	for _, providerID := range changed {
		if err := records.TerminateSessions(ctx, providerID, change.AtMS); err != nil {
			return fmt.Errorf("terminate changed provider sessions: %w", err)
		}
	}
	if err := records.Publish(ctx, change); err != nil {
		return fmt.Errorf("publish runtime catalog: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("create runtime reconciliation audit ID: %w", err)
	}
	diff, err := json.Marshal(map[string]any{
		"catalogSha256": change.Candidate.CatalogSHA256,
		"providers":     changed,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", service.ErrProjectionInvalid, err)
	}
	if err := records.Audit(ctx, service.Audit{
		ID: id.String(), DiffJSON: diff, AtMS: change.AtMS,
	}); err != nil {
		return fmt.Errorf("audit runtime reconciliation: %w", err)
	}
	return nil
}

func validateProviderVersion(
	candidate runtimebundle.ActiveProvider,
	current service.CurrentProvider,
	exists bool,
) (bool, error) {
	if !exists {
		return true, nil
	}
	comparison := semver.Compare("v"+candidate.ProviderVersion, "v"+current.Version)
	if comparison < 0 {
		return false, fmt.Errorf("%w: %s", service.ErrProviderDowngrade, candidate.ProviderID)
	}
	if comparison == 0 && candidate.BundleSHA256 != current.BundleSHA256 {
		return false, fmt.Errorf(
			"%w: %s", service.ErrProviderVersionRebuilt, candidate.ProviderID,
		)
	}
	return comparison > 0 || candidate.BundleSHA256 != current.BundleSHA256, nil
}

func validateCheckpointFormats(
	providerID string, target service.TargetProjection, formats []string,
) error {
	readable := make(map[string]bool)
	if target.Target.Checkpoint != nil {
		for _, format := range target.Target.Checkpoint.ReadFormats {
			readable[format] = true
		}
	}
	for _, format := range formats {
		if !readable[format] {
			return fmt.Errorf(
				"%w: %s/%s %s",
				service.ErrProviderCheckpointUnreadable,
				providerID, target.Target.ID, format,
			)
		}
	}
	return nil
}
