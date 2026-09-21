package emulationstationimport

import (
	"context"
	"encoding/hex"

	"retrom/internal/blobstore"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
	application "retrom/internal/service/emulationstationimport"
)

type SourceGuard interface {
	Check(context.Context, application.Execution) error
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
		roots[entry.ID] = Root{ID: entry.ID, Label: entry.Label, path: entry.Path, digest: hex.EncodeToString(digest[:])}
	}
	return &Sources{blobs: blobs, roots: roots, guard: guard, diagnostics: diagnostics}
}

func (source *Sources) CheckRoot(unit application.Execution) error {
	root, found := source.roots[unit.RootID]
	if !found || root.digest != unit.RootDigest {
		return application.ErrExecutionRootChanged
	}
	return nil
}

func (source *Sources) ForScan(unit application.Execution) (application.ScannerSource, error) {
	if err := source.CheckRoot(unit); err != nil {
		return nil, err
	}
	return scannerSource{root: source.roots[unit.RootID], selectedPath: unit.RelativePath}, nil
}

func (source *Sources) CopyFile(
	ctx context.Context,
	unit application.Execution,
	file application.ExecutionFile,
) (application.VerifiedBlob, error) {
	root, found := source.roots[unit.RootID]
	if !found || root.digest != unit.RootDigest {
		return application.VerifiedBlob{}, application.ErrSourceChanged
	}
	metadata, err := source.copySource(ctx, root, unit, unit.RelativePath, file.Path, file.Size, file.Facts)
	if err != nil {
		return application.VerifiedBlob{}, err
	}
	return verifiedBlob(metadata), nil
}

func (source *Sources) CopyAsset(
	ctx context.Context,
	unit application.Execution,
	asset application.ExecutionAsset,
) (application.VerifiedBlob, bool, error) {
	root, found := source.roots[unit.RootID]
	if !found || root.digest != unit.RootDigest {
		return application.VerifiedBlob{}, false, application.ErrSourceChanged
	}
	metadata, valid, err := source.copyAsset(ctx, root, unit, unit.RelativePath, asset)
	if err != nil {
		return application.VerifiedBlob{}, false, err
	}
	return verifiedBlob(metadata), valid, nil
}

func (source *Sources) DatabaseCause(err error) string { return source.diagnostics.DatabaseCause(err) }

func verifiedBlob(metadata blobstore.Metadata) application.VerifiedBlob {
	return application.VerifiedBlob{
		SHA256: metadata.SHA256,
		MD5:    metadata.MD5,
		SHA1:   metadata.SHA1,
		CRC32:  metadata.CRC32,
		Size:   metadata.Size,
	}
}
