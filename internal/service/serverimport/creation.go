package serverimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

type Creation struct {
	repository CreationRepository
	sources    SourceSelector
	now        func() time.Time
}

func NewCreation(repository CreationRepository, sources SourceSelector, now func() time.Time) *Creation {
	return &Creation{repository: repository, sources: sources, now: now}
}

func (service *Creation) Create(ctx context.Context, request CreateRequest, actorID string) (Summary, error) {
	if request.Kind != "BIOS_DIRECTORY" {
		return Summary{}, ErrCatalogInvalid
	}
	root, err := service.sources.Select(ctx, request.RootID, request.SourceRelativePath)
	if err != nil {
		return Summary{}, fmt.Errorf("select import source: %w", err)
	}
	items, digest, err := service.freezeCatalog(ctx)
	if err != nil {
		return Summary{}, err
	}
	plan, err := newCreationPlan(request, actorID, root, items, digest, service.now().UnixMilli())
	if err != nil {
		return Summary{}, err
	}
	var result Summary
	err = service.repository.WithCreate(ctx, func(writer CreationWriter) error {
		active, err := writer.Active(ctx, request.Kind)
		if err != nil {
			return fmt.Errorf("check active imports: %w", err)
		}
		if active {
			return ErrActive
		}
		result, err = writer.Insert(ctx, plan)
		if err != nil {
			return fmt.Errorf("persist import creation: %w", err)
		}
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("create server import: %w", err)
	}
	return result, nil
}

func (service *Creation) freezeCatalog(ctx context.Context) ([]CatalogItem, string, error) {
	entries, err := service.repository.Catalog(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("read import catalog: %w", err)
	}
	if len(entries) == 0 {
		return nil, "", ErrCatalogEmpty
	}
	items := make([]CatalogItem, 0, len(entries))
	for _, entry := range entries {
		if err := entry.Item.ValidateSource(entry.DATReady); err != nil {
			return nil, "", err
		}
		items = append(items, entry.Item)
	}
	slices.SortFunc(items, func(a, b CatalogItem) int { return strings.Compare(a.RequirementID, b.RequirementID) })
	encoded, err := CanonicalCatalogJSON(items)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(encoded)
	return items, hex.EncodeToString(digest[:]), nil
}
