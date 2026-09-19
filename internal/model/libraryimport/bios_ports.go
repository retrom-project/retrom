package libraryimport

import (
	"context"

	contentvalidation "retrom/internal/capability/content/corevalidation"
	"retrom/internal/model/corevalidation"
)

// BIOSFactsReader reads current BIOS facts in the caller's review or creation scope.
type BIOSFactsReader interface {
	BIOS(context.Context, string, string) ([]corevalidation.BIOSRecord, error)
}

// CreationBIOSReader supplies import configuration and validation facts.
type CreationBIOSReader interface {
	BIOSFactsReader
	Catalog(context.Context, string, string) ([]contentvalidation.BIOSCatalogEntry, error)
}
