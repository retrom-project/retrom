package runtimeprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"retrom/internal/capability/runtime/runtimebundle"
	service "retrom/internal/model/runtimeprovider"
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
	auditID string,
) error {
	for _, providerID := range changed {
		if err := records.TerminateSessions(ctx, providerID, change.AtMS); err != nil {
			return fmt.Errorf("terminate changed provider sessions: %w", err)
		}
	}
	if err := records.Publish(ctx, change); err != nil {
		return fmt.Errorf("publish runtime catalog: %w", err)
	}
	diff, err := json.Marshal(map[string]any{
		"catalogSha256": change.Candidate.CatalogSHA256,
		"providers":     changed,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", service.ErrProjectionInvalid, err)
	}
	if err := records.Audit(ctx, service.Audit{
		ID: auditID, DiffJSON: diff, AtMS: change.AtMS,
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
	changed, err := service.ValidateProviderVersionChange(
		candidate.ProviderVersion, candidate.BundleSHA256,
		current.Version, current.BundleSHA256,
		candidate.ProviderID, exists,
	)
	if err != nil {
		return false, fmt.Errorf("validate provider version: %w", err)
	}
	return changed, nil
}

func validateCheckpointFormats(
	providerID string, target service.TargetProjection, formats []string,
) error {
	var readFormats []string
	if target.Target.Checkpoint != nil {
		readFormats = target.Target.Checkpoint.ReadFormats
	}
	if err := service.ValidateCheckpointFormats(
		providerID, target.Target.ID, readFormats, formats,
	); err != nil {
		return fmt.Errorf("validate checkpoint formats: %w", err)
	}
	return nil
}
