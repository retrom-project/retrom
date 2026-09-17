package serverimport

import (
	"context"
	"errors"
)

var (
	ErrActive         = errors.New("SERVER_BIOS_IMPORT_ACTIVE")
	ErrCatalogEmpty   = errors.New("BIOS_CATALOG_EMPTY")
	ErrCatalogInvalid = errors.New("BIOS_CATALOG_INVALID")
)

type CreateRequest struct {
	Kind               string `json:"kind"`
	RootID             string `json:"rootId"`
	SourceRelativePath string `json:"sourceRelativePath"`
	ReplaceIfBetter    bool   `json:"replaceIfBetter"`
}

type (
	RootSelection struct{ ID, Label, Digest string }
	CatalogEntry  struct {
		Item     CatalogItem
		DATReady bool
	}
)

type CreationPlan struct {
	ImportID, JobID, DedupeKey string
	Request                    CreateRequest
	Root                       RootSelection
	Items                      []CatalogItem
	CatalogDigest, InputDigest string
	Input, Payload, Audit      []byte
	Evidence                   ControlEvidence
}

type SourceSelector interface {
	Select(context.Context, string, string) (RootSelection, error)
}

type CreationRepository interface {
	Catalog(context.Context) ([]CatalogEntry, error)
	CommitCreate(context.Context, CreationPlan) (Summary, error)
}
