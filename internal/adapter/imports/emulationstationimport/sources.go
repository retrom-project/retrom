package emulationstationimport

import (
	"context"
	"encoding/hex"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

type SourceGuard interface {
	Check(context.Context, emulationstationimportmodel.Execution) error
}
type (
	SourceDiagnostics interface{ DatabaseCause(error) string }
	Sources           struct {
		blobs       *blobstore.Store
		roots       map[string]Root
		guard       SourceGuard
		diagnostics SourceDiagnostics
	}
)

func NewSources(
	blobs *blobstore.Store,
	credentials *retromruntime.Credentials,
	configured []serversource.Root,
	guard SourceGuard,
	diagnostics SourceDiagnostics,
) *Sources {
	roots := make(map[string]Root, len(configured))
	for _, entry := range configured {
		digest := credentials.ServerImportRootDigest(entry.ID, entry.Path)
		roots[entry.ID] = Root{
			ID:     entry.ID,
			Label:  entry.Label,
			path:   entry.Path,
			digest: hex.EncodeToString(digest[:]),
		}
	}
	return &Sources{blobs: blobs, roots: roots, guard: guard, diagnostics: diagnostics}
}

func (source *Sources) CheckRoot(unit emulationstationimportmodel.Execution) error {
	root, found := source.roots[unit.RootID]
	if !found || root.digest != unit.RootDigest {
		return emulationstationimportservice.ErrExecutionRootChanged
	}
	return nil
}

func (source *Sources) ForScan(
	unit emulationstationimportmodel.Execution,
) (emulationstationimportservice.ScannerSource, error) {
	if err := source.CheckRoot(unit); err != nil {
		return nil, err
	}
	return scannerSource{root: source.roots[unit.RootID], selectedPath: unit.RelativePath}, nil
}

func (source *Sources) CopyFile(
	ctx context.Context,
	unit emulationstationimportmodel.Execution,
	file emulationstationimportmodel.ExecutionFile,
) (emulationstationimportmodel.VerifiedBlob, error) {
	root, found := source.roots[unit.RootID]
	if !found || root.digest != unit.RootDigest {
		return emulationstationimportmodel.VerifiedBlob{}, emulationstationimportmodel.ErrSourceChanged
	}
	metadata, err := source.copySource(ctx, root, unit, unit.RelativePath, file.Path, file.Size, file.Facts)
	if err != nil {
		return emulationstationimportmodel.VerifiedBlob{}, err
	}
	return verifiedBlob(metadata), nil
}

func (source *Sources) CopyAsset(
	ctx context.Context,
	unit emulationstationimportmodel.Execution,
	asset emulationstationimportmodel.ExecutionAsset,
) (emulationstationimportmodel.VerifiedBlob, bool, error) {
	root, found := source.roots[unit.RootID]
	if !found || root.digest != unit.RootDigest {
		return emulationstationimportmodel.VerifiedBlob{}, false, emulationstationimportmodel.ErrSourceChanged
	}
	metadata, valid, err := source.copyAsset(ctx, root, unit, unit.RelativePath, asset)
	if err != nil {
		return emulationstationimportmodel.VerifiedBlob{}, false, err
	}
	return verifiedBlob(metadata), valid, nil
}

func (source *Sources) DatabaseCause(err error) string { return source.diagnostics.DatabaseCause(err) }

func verifiedBlob(metadata blobstore.Metadata) emulationstationimportmodel.VerifiedBlob {
	return emulationstationimportmodel.VerifiedBlob{
		SHA256: metadata.SHA256,
		MD5:    metadata.MD5,
		SHA1:   metadata.SHA1,
		CRC32:  metadata.CRC32,
		Size:   metadata.Size,
	}
}
