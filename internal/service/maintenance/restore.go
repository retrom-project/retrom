package maintenance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"retrom/internal/config"
	"retrom/internal/dependencies"
)

func (service *Service) Restore(
	ctx context.Context,
	configuration config.Maintenance,
	input, output string,
) (Manifest, error) {
	if !filepath.IsAbs(input) || !filepath.IsAbs(output) || filepath.Clean(input) != input ||
		filepath.Clean(output) != output ||
		exists(output) {
		return Manifest{}, ErrInvalidBundle
	}
	lineage, err := service.repository.CurrentLineage()
	if err != nil {
		return Manifest{}, fmt.Errorf("read restore schema lineage: %w", err)
	}
	manifest, err := validateBundle(input, lineage)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateRestoreDependencies(configuration, manifest); err != nil {
		return Manifest{}, err
	}
	staging, err := createStaging(output)
	if err != nil {
		return Manifest{}, err
	}
	if err := copyRestoreFiles(input, staging, manifest); err != nil {
		return Manifest{}, err
	}
	if err := service.validateAndFenceRestore(ctx, staging); err != nil {
		return Manifest{}, err
	}
	if err := publishRestore(staging, output); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateRestoreDependencies(configuration config.Maintenance, manifest Manifest) error {
	if strings.Join(configuration.DependencyVersions, ",") != strings.Join(manifest.DependencyVersions, ",") ||
		configuration.ActiveEJSVersion != manifest.ActiveEmulatorjsVersion {
		return ErrDependencyMismatch
	}
	if _, err := dependencies.Load(
		configuration.DependencyRoot,
		configuration.DependencyVersions,
		configuration.ActiveEJSVersion,
	); err != nil {
		return ErrDependencyMismatch
	}
	for _, evidence := range manifest.DependencyManifests {
		manifestRoot := filepath.Join(configuration.DependencyRoot, "dat", "emulatorjs", evidence.Version)
		for _, value := range []struct{ source, expected string }{
			{filepath.Join(manifestRoot, "manifest.json"), evidence.ManifestSHA256},
			{filepath.Join(manifestRoot, "SHA256SUMS"), evidence.SHA256SumsSHA256},
		} {
			if digest, _, err := digestRegular(value.source); err != nil || digest != value.expected {
				return ErrDependencyMismatch
			}
		}
	}
	return nil
}

func copyRestoreFiles(input, staging string, manifest Manifest) error {
	for _, entry := range manifest.Files {
		if entry.Kind == "DEPENDENCY_MANIFEST" || entry.Kind == "DEPENDENCY_SHA256SUMS" {
			continue
		}
		if _, err := copyVerified(
			filepath.Join(input, filepath.FromSlash(entry.Path)),
			filepath.Join(staging, filepath.FromSlash(entry.Path)),
			entry.Path,
			entry.Kind,
			entry.SHA256,
		); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) validateAndFenceRestore(ctx context.Context, staging string) error {
	path := filepath.Join(staging, "retrom.db")
	snapshot, err := service.repository.Inspect(ctx, path)
	if err != nil {
		return fmt.Errorf("inspect restored database: %w", err)
	}
	if err := validateRestoredContents(ctx, snapshot, staging); err != nil {
		return err
	}
	return service.fenceRestore(ctx, path)
}

func publishRestore(staging, output string) error {
	if err := syncTree(staging); err != nil {
		return err
	}
	if err := os.Rename(staging, output); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := syncDirectory(filepath.Dir(output)); err != nil {
		return err
	}
	return nil
}
