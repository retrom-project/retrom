package libraryimport

import (
	"context"

	"retrom/internal/capability/engine/scummvm"
)

type ImportPreparationOptions struct {
	MultiDiscEnabled, MetadataScraperAvailable bool
	ScummVMDetector                            *scummvm.Detector
}

type PreparedImport struct {
	Request                               ImportRequest
	Upload                                ImportUpload
	Target                                ImportTarget
	ContentMode, SourceType, DATVersionID string
	Files                                 []ImportFile
	Dispositions                          []PreparedDisposition
	Groups                                []PreparedGroup
	Archives                              []PreparedArchive
}

type ImportPreparationCatalog interface {
	ActiveDAT(context.Context, string, string) (string, error)
	MachineClassification(context.Context, string, string) (string, bool, error)
	ArcadeRequirements(context.Context, string, string) (ArcadeCatalogRequirements, error)
	MachineRelation(context.Context, string, string) (ArcadeMachineRelation, bool, error)
}

type ArcadeROMRequirement struct {
	Size                   int64
	CRC32, SHA1, MergeName *string
	Name, Status           string
	BIOSName               *string
}

type ArcadeCatalogRequirements struct {
	DefaultBIOS *string
	ROMs        []ArcadeROMRequirement
	HasDisk     bool
}
