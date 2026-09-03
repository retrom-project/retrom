package runtimeprovider

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimelaunch"
)

const metadataLimit = 16 << 20

var (
	errMetadataFile    = errors.New("metadata is not a bounded regular file")
	errMetadataSymlink = errors.New("runtime provider metadata symlink forbidden")
)

// Paths names the three immutable startup inputs. All bundle reads are rooted
// beneath InstalledRoot; no network or archive operation is performed here.
type Paths struct {
	ActivePath    string
	InstalledRoot string
	CatalogPath   string
}

// Installation is the completely verified runtime snapshot used by one
// process. It is safe to reconcile and expose only after LoadInstallation
// returns successfully.
type Installation struct {
	Active     runtimebundle.ActiveDescriptor
	Manifests  map[string]runtimebundle.Manifest
	Integrity  map[string][]runtimebundle.IntegrityFile
	Catalog    runtimecatalog.Catalog
	Projection Projection
	Handler    http.Handler
	Builder    *runtimelaunch.Builder
}

func LoadInstallation(paths Paths) (Installation, error) {
	activeContents, err := readMetadata(paths.ActivePath)
	if err != nil {
		return Installation{}, installationInvalid(err)
	}
	active, err := runtimebundle.ParseActiveDescriptor(activeContents)
	if err != nil {
		return Installation{}, installationInvalid(err)
	}
	root, err := filepath.Abs(paths.InstalledRoot)
	if err != nil {
		return Installation{}, installationInvalid(err)
	}
	manifests, integrityByProvider, err := loadInstalledProviders(root, active)
	if err != nil {
		return Installation{}, err
	}
	catalogContents, err := readMetadata(paths.CatalogPath)
	if err != nil {
		return Installation{}, installationInvalid(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(catalogContents)
	if err != nil {
		return Installation{}, installationInvalid(err)
	}
	projection, err := NewProjection(active, manifests, catalog)
	if err != nil {
		return Installation{}, installationInvalid(err)
	}
	builder, err := runtimelaunch.NewBuilder(active, manifests)
	if err != nil {
		return Installation{}, installationInvalid(err)
	}
	handler, err := NewStaticHandler(root, active, integrityByProvider)
	if err != nil {
		return Installation{}, err
	}
	return Installation{
		Active: active, Manifests: manifests, Integrity: integrityByProvider,
		Catalog: catalog, Projection: projection, Handler: handler, Builder: builder,
	}, nil
}

func loadInstalledProviders(
	root string,
	active runtimebundle.ActiveDescriptor,
) (map[string]runtimebundle.Manifest, map[string][]runtimebundle.IntegrityFile, error) {
	manifests := make(map[string]runtimebundle.Manifest, len(active.Providers))
	integrityByProvider := make(map[string][]runtimebundle.IntegrityFile, len(active.Providers))
	for _, provider := range active.Providers {
		manifest, integrityFiles, err := loadInstalledProvider(root, provider)
		if err != nil {
			return nil, nil, err
		}
		manifests[provider.ProviderID] = manifest
		integrityByProvider[provider.ProviderID] = integrityFiles
	}
	return manifests, integrityByProvider, nil
}

func loadInstalledProvider(
	root string,
	provider runtimebundle.ActiveProvider,
) (runtimebundle.Manifest, []runtimebundle.IntegrityFile, error) {
	directory := filepath.Join(root, filepath.FromSlash(provider.InstallationPath))
	if !pathWithin(root, directory) {
		return runtimebundle.Manifest{}, nil, ErrInstallationInvalid
	}
	manifestContents, err := readMetadata(filepath.Join(directory, "provider.json"))
	if err != nil || digest(manifestContents) != provider.ManifestSHA256 {
		return runtimebundle.Manifest{}, nil, ErrInstallationInvalid
	}
	manifest, err := runtimebundle.ParseManifest(manifestContents)
	if err != nil {
		return runtimebundle.Manifest{}, nil, installationInvalid(err)
	}
	integrityContents, err := readMetadata(filepath.Join(directory, "integrity.json"))
	if err != nil {
		return runtimebundle.Manifest{}, nil, installationInvalid(err)
	}
	integrity, err := runtimebundle.ParseIntegrity(integrityContents)
	if err != nil {
		return runtimebundle.Manifest{}, nil, installationInvalid(err)
	}
	manifest, err = runtimebundle.BindTargetIntegrity(manifest, integrity.Files)
	if err != nil {
		return runtimebundle.Manifest{}, nil, installationInvalid(err)
	}
	if !installedProviderMatches(provider, manifest, integrity.Files, int64(len(integrityContents))) {
		return runtimebundle.Manifest{}, nil, ErrInstallationInvalid
	}
	return manifest, integrity.Files, nil
}

func installedProviderMatches(
	provider runtimebundle.ActiveProvider,
	manifest runtimebundle.Manifest,
	files []runtimebundle.IntegrityFile,
	integritySize int64,
) bool {
	if manifest.ProviderID != provider.ProviderID || manifest.ProviderVersion != provider.ProviderVersion ||
		manifest.ProviderAPI != provider.ProviderAPI || manifest.ClientModulePath != provider.ClientModulePath ||
		int64(len(files)+1) != provider.FileCount {
		return false
	}
	unpackedSize := integritySize
	for _, file := range files {
		if file.SizeBytes > provider.UnpackedSizeBytes-unpackedSize {
			return false
		}
		unpackedSize += file.SizeBytes
	}
	return unpackedSize == provider.UnpackedSizeBytes
}

func (installation Installation) Reconcile(ctx context.Context, database *sql.DB, now time.Time) error {
	return Reconcile(ctx, database, installation.Projection, now)
}

func readMetadata(path string) ([]byte, error) {
	metadata, err := os.Lstat(path)
	if err != nil || !metadata.Mode().IsRegular() || metadata.Size() < 2 || metadata.Size() > metadataLimit {
		if err == nil {
			err = errMetadataFile
		}
		return nil, fmt.Errorf("read runtime provider metadata: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(path) {
		return nil, errMetadataSymlink
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read runtime provider metadata: %w", err)
	}
	return contents, nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) &&
		len(relative) > 0 && relative[0] != '.'
}

func digest(contents []byte) string {
	value := sha256.Sum256(contents)
	return hex.EncodeToString(value[:])
}
