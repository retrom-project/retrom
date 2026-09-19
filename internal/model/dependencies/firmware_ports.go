package dependencies

import "retrom/internal/capability/content/firmwaremanifest"

// FirmwareCatalogSource acquires the pinned declarations before any write scope.
type FirmwareCatalogSource interface {
	LoadCatalog() (firmwaremanifest.Catalog, error)
}
