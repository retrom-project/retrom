package arcade

import (
	"context"
	"errors"

	corevalidation "retrom/internal/core/validation"
)

var ErrInvalid = errors.New("ARCADE_CATALOG_INVALID")

type Catalog interface {
	MachineClassification(context.Context, string, string) (string, bool, error)
	ArcadeRequirements(context.Context, string, string) (CatalogRequirements, error)
	MachineRelation(context.Context, string, string) (MachineRelation, bool, error)
}

type ROMRequirement struct {
	Size                   int64
	CRC32, SHA1, MergeName *string
	Name, Status           string
	BIOSName               *string
}
type CatalogRequirements struct {
	DefaultBIOS *string
	ROMs        []ROMRequirement
	HasDisk     bool
}
type BIOSReader interface {
	BIOS(context.Context, string, string, string) (corevalidation.BIOSDependency, bool, error)
}

// NoInstalledBIOS evaluates only content-provided dependencies.
// Installed BIOS is resolved by the caller's later admission.
type NoInstalledBIOS struct{}

func (NoInstalledBIOS) BIOS(context.Context, string, string, string) (corevalidation.BIOSDependency, bool, error) {
	return corevalidation.BIOSDependency{}, false, nil
}

type Resource struct {
	Role, LogicalName, FileRecord string
	SortOrder                     int
}
