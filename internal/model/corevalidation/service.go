package corevalidation

import (
	"context"

	contentvalidation "retrom/internal/capability/content/corevalidation"
)

// Repository is the storage-facing contract used by the core-validation
// service. The contract lives in model so repositories and services share the
// same boundary without a dependency from repo back to service.
type Repository interface {
	Catalog(context.Context, string, string) ([]contentvalidation.BIOSCatalogEntry, error)
	BIOS(context.Context, string, string) ([]BIOSRecord, error)
}

type BIOSRecord struct {
	Dependency        contentvalidation.BIOSDependency
	ActivationOptions *string
}
