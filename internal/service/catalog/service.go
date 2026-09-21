package catalog

import (
	"context"
	"fmt"

	"retrom/internal/contentcapability"
	"retrom/internal/contentprofile"
)

type PlatformTargetSupport interface {
	SupportsPlatformTarget(platformID, coreID, providerID, targetID string) bool
}

type Service struct {
	repository Repository
	netplay    PlatformTargetSupport
}

func New(repository Repository, netplay PlatformTargetSupport) *Service {
	return &Service{repository: repository, netplay: netplay}
}

func (service *Service) WithNetplay(netplay PlatformTargetSupport) *Service {
	service.netplay = netplay
	return service
}

type Platform struct {
	ID, Name  string
	SortOrder int
	Enabled   bool
	Cores     []PlatformCore
}

type PlatformCore struct {
	ID, Name         string
	Enabled          bool
	NetplaySupported bool
}

type (
	RuntimeTargetView    = RuntimeTarget
	PlatformInstanceView struct {
		PlatformInstance
		SupportedExtensions []string
		ImportCapabilities  contentcapability.ImportCapabilities
	}
)

func (service *Service) Platforms(ctx context.Context) ([]Platform, error) {
	rows, err := service.repository.Platforms(ctx)
	if err != nil {
		return nil, fmt.Errorf("read platforms: %w", err)
	}
	items := make([]Platform, 0, len(rows))
	byID := make(map[string]int, len(rows))
	cores := make([]map[string]int, 0, len(rows))
	for _, row := range rows {
		index, exists := byID[row.ID]
		if !exists {
			index = len(items)
			byID[row.ID] = index
			items = append(items, Platform{ID: row.ID, Name: row.Name, SortOrder: row.SortOrder, Enabled: row.Enabled})
			cores = append(cores, make(map[string]int))
		}
		if row.CoreID == nil {
			continue
		}
		coreID := *row.CoreID
		if coreIndex, exists := cores[index][coreID]; exists {
			if service.supports(row, items[index].ID, coreID) {
				items[index].Cores[coreIndex].NetplaySupported = true
			}
			continue
		}
		core := PlatformCore{ID: coreID}
		if row.CoreName != nil {
			core.Name = *row.CoreName
		}
		if row.CoreEnabled != nil {
			core.Enabled = *row.CoreEnabled
		}
		core.NetplaySupported = service.supports(row, items[index].ID, coreID)
		cores[index][coreID] = len(items[index].Cores)
		items[index].Cores = append(items[index].Cores, core)
	}
	return items, nil
}

func (service *Service) supports(row PlatformRow, platformID, coreID string) bool {
	return service.netplay != nil && row.ProviderID != nil && row.TargetID != nil &&
		service.netplay.SupportsPlatformTarget(platformID, coreID, *row.ProviderID, *row.TargetID)
}

func (service *Service) RuntimeTargets(ctx context.Context) ([]RuntimeTargetView, error) {
	items, err := service.repository.RuntimeTargets(ctx)
	if err != nil {
		return nil, fmt.Errorf("read runtime targets: %w", err)
	}
	return items, nil
}

func (service *Service) PlatformInstances(
	ctx context.Context,
	query PlatformInstanceQuery,
	featureEnabled bool,
) ([]PlatformInstanceView, error) {
	items, err := service.repository.PlatformInstances(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read platform instances: %w", err)
	}
	result := make([]PlatformInstanceView, 0, len(items))
	for _, item := range items {
		result = append(result, PlatformInstanceView{
			PlatformInstance:    item,
			SupportedExtensions: contentprofile.SupportedExtensions(item.PlatformID),
			ImportCapabilities: contentcapability.Resolve(
				item.PlatformID, item.Enabled, featureEnabled, item.ContentPolicy,
			),
		})
	}
	return result, nil
}
