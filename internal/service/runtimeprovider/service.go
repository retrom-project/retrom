package runtimeprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }
func (service *Service) Reconcile(ctx context.Context, candidate Projection, now time.Time) error {
	if len(candidate.CatalogSHA256) != 64 || len(candidate.Providers) == 0 || now.UnixMilli() < 0 {
		return ErrProjectionInvalid
	}
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		current, err := scope.Catalog.Current(ctx)
		if err != nil {
			return fmt.Errorf("read current runtime catalog: %w", err)
		}
		change, changed, err := prepareReconciliation(ctx, scope.Catalog, current, candidate, now.UnixMilli())
		if err != nil {
			return err
		}
		if len(changed) == 0 && current.CatalogSHA256 == candidate.CatalogSHA256 {
			return nil
		}
		return applyChangedProjection(ctx, scope.Projection, change, changed)
	})
	if err != nil {
		return fmt.Errorf("reconcile runtime providers: %w", err)
	}
	return nil
}

func applyChangedProjection(
	ctx context.Context,
	records ProjectionRecords,
	change Publication,
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
	diff, err := json.Marshal(map[string]any{"catalogSha256": change.Candidate.CatalogSHA256, "providers": changed})
	if err != nil {
		return projectionInvalid(err)
	}
	if err := records.Audit(ctx, Audit{ID: id.String(), DiffJSON: diff, AtMS: change.AtMS}); err != nil {
		return fmt.Errorf("audit runtime reconciliation: %w", err)
	}
	return nil
}

func prepareReconciliation(
	ctx context.Context,
	records CatalogRecords,
	current CurrentState,
	candidate Projection,
	now int64,
) (Publication, []string, error) {
	result := Publication{Candidate: candidate, AtMS: now}
	providers := make(map[string]bool)
	targets := make(map[TargetIdentity]bool)
	changed := make([]string, 0, len(candidate.Providers))
	for _, provider := range candidate.Providers {
		id := provider.Active.ProviderID
		providers[id] = true
		previous, exists := current.Providers[id]
		updated, err := validateProviderVersion(provider.Active, previous, exists)
		if err != nil {
			return Publication{}, nil, err
		}
		if updated {
			changed = append(changed, id)
		}
		for _, target := range provider.Targets {
			identity := TargetIdentity{ProviderID: id, TargetID: target.Target.ID}
			targets[identity] = true
			formats, err := records.CheckpointFormats(ctx, identity)
			if err != nil {
				return Publication{}, nil, fmt.Errorf("read stored checkpoint formats: %w", err)
			}
			if err := ValidateCheckpointFormats(id, target, formats); err != nil {
				return Publication{}, nil, err
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
			return Publication{}, nil, fmt.Errorf("inspect removed runtime target: %w", err)
		}
		if referenced {
			return Publication{}, nil, fmt.Errorf("%w: %s/%s", ErrProviderTargetReferenced, target.ProviderID, target.TargetID)
		}
		result.RemovedTargets = append(result.RemovedTargets, target)
	}
	sort.Strings(changed)
	sort.Strings(result.RemovedProviders)
	return result, changed, nil
}

func ValidateCheckpointFormats(providerID string, target TargetProjection, formats []string) error {
	readable := make(map[string]bool)
	if target.Target.Checkpoint != nil {
		for _, format := range target.Target.Checkpoint.ReadFormats {
			readable[format] = true
		}
	}
	for _, format := range formats {
		if !readable[format] {
			return fmt.Errorf("%w: %s/%s %s", ErrProviderCheckpointUnreadable, providerID, target.Target.ID, format)
		}
	}
	return nil
}
