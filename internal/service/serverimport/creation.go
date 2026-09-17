package serverimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	model "retrom/internal/model/serverimport"
)

type Creation struct {
	repository model.CreationRepository
	sources    model.SourceSelector
	now        func() time.Time
}

func NewCreation(
	repository model.CreationRepository,
	sources model.SourceSelector,
	now func() time.Time,
) *Creation {
	return &Creation{repository: repository, sources: sources, now: now}
}

func (service *Creation) Create(
	ctx context.Context,
	request model.CreateRequest,
	actorID string,
) (model.Summary, error) {
	if request.Kind != "BIOS_DIRECTORY" {
		return model.Summary{}, model.ErrCatalogInvalid
	}
	root, err := service.sources.Select(
		ctx, request.RootID, request.SourceRelativePath,
	)
	if err != nil {
		return model.Summary{},
			fmt.Errorf("select import source: %w", err)
	}
	items, digest, err := service.freezeCatalog(ctx)
	if err != nil {
		return model.Summary{}, err
	}
	plan, err := newCreationPlan(
		request, actorID, root, items, digest, service.now().UnixMilli(),
	)
	if err != nil {
		return model.Summary{}, err
	}
	result, err := service.repository.CommitCreate(ctx, plan)
	if err != nil {
		return model.Summary{},
			fmt.Errorf("create server import: %w", err)
	}
	return result, nil
}

func (service *Creation) freezeCatalog(
	ctx context.Context,
) ([]model.CatalogItem, string, error) {
	entries, err := service.repository.Catalog(ctx)
	if err != nil {
		return nil, "",
			fmt.Errorf("read import catalog: %w", err)
	}
	if len(entries) == 0 {
		return nil, "", model.ErrCatalogEmpty
	}
	items := make([]model.CatalogItem, 0, len(entries))
	for _, entry := range entries {
		if err := entry.Item.ValidateSource(entry.DATReady); err != nil {
			return nil, "",
				fmt.Errorf("validate catalog item %s: %w",
					entry.Item.RequirementID, err)
		}
		items = append(items, entry.Item)
	}
	slices.SortFunc(
		items,
		func(a, b model.CatalogItem) int {
			return strings.Compare(a.RequirementID, b.RequirementID)
		},
	)
	encoded, err := model.CanonicalCatalogJSON(items)
	if err != nil {
		return nil, "",
			fmt.Errorf("encode canonical catalog: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return items, hex.EncodeToString(digest[:]), nil
}
