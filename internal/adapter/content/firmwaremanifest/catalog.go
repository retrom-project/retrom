package firmwaremanifest

import (
	_ "embed"
	"fmt"

	firmwarepolicy "retrom/internal/capability/content/firmwaremanifest"
	dependencymodel "retrom/internal/model/dependencies"
)

//go:embed catalog.json
var catalogJSON []byte

// Source provides the pinned firmware declarations embedded by the application.
type Source struct{}

var _ dependencymodel.FirmwareCatalogSource = Source{}

func (Source) LoadCatalog() (firmwarepolicy.Catalog, error) {
	catalog, err := firmwarepolicy.Parse(catalogJSON)
	if err != nil {
		return catalog, fmt.Errorf("%w", err)
	}
	return catalog, nil
}
