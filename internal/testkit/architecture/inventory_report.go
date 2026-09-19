package architecture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrSourceChanged = errors.New("architecture: source changed during analysis")

// InventoryReport keeps coverage failures separate from successful type analysis.
type InventoryReport struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	Baseline       BaselineReport       `json:"baseline"`
	Sources        SourceSnapshot       `json:"sources"`
	CompiledInputs SourceSnapshot       `json:"compiledInputs"`
	Configuration  SourceSnapshot       `json:"configuration"`
	Compatibility  CompatibilityReport  `json:"compatibility"`
	GoSources      []GoSourceInventory  `json:"goSources"`
	GoPackages     []GoPackageInventory `json:"goPackages"`
	Violations     []Violation          `json:"violations"`
}

// InspectRepository only reads source and registry inputs.
func InspectRepository(ctx context.Context, root string) (InventoryReport, error) {
	registry, err := LoadOwnership(root)
	if err != nil {
		return InventoryReport{}, err
	}
	baseline, err := InspectBaseline(ctx, root, registry.Baseline)
	if err != nil {
		return InventoryReport{}, err
	}
	sources, err := DiscoverSources(ctx, root)
	if err != nil {
		return InventoryReport{}, err
	}
	snapshot, err := SnapshotFiles(root, sources)
	if err != nil {
		return InventoryReport{}, err
	}
	config, err := SnapshotFiles(root, []string{
		"go.mod", "go.sum", "Makefile", "web/package.json", "web/package-lock.json", "web/tsconfig.json",
		"quality/architecture/package-ownership.json", "quality/architecture/characterizations.json",
	})
	if err != nil {
		return InventoryReport{}, err
	}
	compatibility, err := InspectCompatibility(ctx, root, registry.Baseline)
	if err != nil {
		return InventoryReport{}, err
	}
	syntax, err := InspectGoSyntax(root, sources)
	if err != nil {
		return InventoryReport{}, err
	}
	graph, err := InspectGo(ctx, root, sources)
	if err != nil {
		return InventoryReport{}, err
	}
	compiled, err := SnapshotFiles(root, CompiledSources(graph))
	if err != nil {
		return InventoryReport{}, err
	}
	if err := verifyUnchangedSourceSet(ctx, root, snapshot); err != nil {
		return InventoryReport{}, err
	}
	if err := verifyUnchangedSources(root, config); err != nil {
		return InventoryReport{}, err
	}
	violations := append(ValidateOwnership(sources, registry), compatibility.Violations...)
	violations = append(violations, ValidateCompilationScope(sources, graph)...)
	return InventoryReport{
		SchemaVersion: 1, Compatibility: compatibility, Baseline: baseline, Configuration: config,
		Sources: snapshot, CompiledInputs: compiled, GoPackages: graph, GoSources: syntax, Violations: violations,
	}, nil
}

func verifyUnchangedSources(root string, before SourceSnapshot) error {
	names := make([]string, 0, len(before.Files))
	for _, file := range before.Files {
		names = append(names, file.Path)
	}
	after, err := SnapshotFiles(root, names)
	if err != nil {
		return err
	}
	if before.SHA256 != after.SHA256 {
		return ErrSourceChanged
	}
	return nil
}

// WriteInventory emits a deterministic JSON report for evidence consumers.
func WriteInventory(output io.Writer, report InventoryReport) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode architecture inventory: %w", err)
	}
	return nil
}

func verifyUnchangedSourceSet(ctx context.Context, root string, before SourceSnapshot) error {
	names, err := DiscoverSources(ctx, root)
	if err != nil {
		return err
	}
	after, err := SnapshotFiles(root, names)
	if err != nil {
		return err
	}
	if after.SHA256 != before.SHA256 {
		return ErrSourceChanged
	}
	return nil
}
