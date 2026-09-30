package arcade

import (
	"context"
	"fmt"

	"retrom/internal/importing"
)

type ArchiveReader interface {
	Entries(context.Context, string) ([]importing.ArchiveEntry, error)
}
type SourceFile struct{ Role, LogicalName, FileRecord string }

// Preparation reads archive bytes outside a writer, then applies the same rules used by import and review.
type Preparation struct {
	catalog  Catalog
	archives ArchiveReader
}

func NewPreparation(catalog Catalog, archives ArchiveReader) *Preparation {
	return &Preparation{catalog: catalog, archives: archives}
}

func (preparation *Preparation) Prepare(
	ctx context.Context, files []SourceFile, datID, machine, primaryName string,
) (Result, error) {
	classification, found, err := preparation.catalog.MachineClassification(ctx, datID, machine)
	if err != nil {
		return Result{}, fmt.Errorf("read arcade primary classification: %w", err)
	}
	if !found || classification != "NORMAL" {
		return Result{Status: "INCOMPATIBLE", Code: "ARCADE_MACHINE_NOT_FOUND"}, nil
	}
	archives := make([]Archive, 0, len(files))
	primaryFound := false
	for _, file := range files {
		primary := file.LogicalName == primaryName && file.Role == "CONTENT"
		entries, err := preparation.archives.Entries(ctx, file.FileRecord)
		if err != nil {
			if primary {
				return Result{Status: "INCOMPATIBLE", Code: "UNSUPPORTED_CONTENT_FORMAT"}, nil
			}
			continue
		}
		archive := Archive{
			Role: "COMPANION", LogicalName: file.LogicalName, FileRecord: file.FileRecord,
			Entries: make(map[string]importing.ArchiveEntry, len(entries)),
		}
		if primary {
			archive.Role = "CONTENT"
			primaryFound = true
		}
		for _, entry := range entries {
			archive.Entries[entry.NormalizedPath] = entry
		}
		archives = append(archives, archive)
	}
	if !primaryFound {
		return Result{Status: "BLOCKED", Code: "ARCADE_CONTENT_MISSING_ENTRY"}, nil
	}
	return Resolve(ctx, preparation.catalog, NoInstalledBIOS{}, "", "", datID, machine, archives)
}
