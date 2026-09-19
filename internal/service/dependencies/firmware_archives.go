package dependencies

import (
	"encoding/json"
	"fmt"

	"retrom/internal/capability/content/firmwaremanifest"
)

func (service *Service) loadStaticBIOSCatalog() ([]staticBIOS, error) {
	if len(service.set.Order) == 0 {
		return nil, nil
	}
	generated, err := service.firmware.LoadCatalog()
	if err != nil {
		return nil, fmt.Errorf("load source firmware catalog: %w", err)
	}
	return completeStaticBIOSCatalog(generated)
}

func completeStaticBIOSCatalog(generated firmwaremanifest.Catalog) ([]staticBIOS, error) {
	catalog := append([]staticBIOS(nil), staticBIOSCatalog...)
	for _, item := range generated.Items {
		members, err := json.Marshal(item.Members)
		if err != nil {
			return nil, fmt.Errorf("encode source firmware members: %w", err)
		}
		catalog = append(catalog, staticBIOS{
			coreID: generated.Source.CoreID, logical: item.LogicalName, mode: item.Mode,
			delivery: "EXTERNAL_FILE", emulatorPath: item.EmulatorPath,
			members: string(members), sourceURL: generated.Source.SourceURL,
			sourceDigest: generated.Source.SourceSHA256 + ":" + generated.ParserVersion,
			providerID:   generated.Source.ProviderID, targetID: generated.Source.TargetID,
		})
	}
	return catalog, nil
}
