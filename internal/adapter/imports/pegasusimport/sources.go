package pegasusimport

import (
	"context"
	"encoding/hex"
	"fmt"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/foundation/cleanup"
	pegasusimportmodel "retrom/internal/model/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

var (
	ErrSourceChanged = pegasusimportmodel.ErrSourceChanged
	ErrScanLimit     = pegasusimportmodel.ErrScanLimit
)

type Root struct {
	ID, Label string
	path      string
	digest    string
}

type Sources struct {
	sourceReader func(context.Context) (func(), error)
	blobs        *blobstore.Store
	roots        map[string]Root
}

func NewSources(
	blobs *blobstore.Store, credentials *retromruntime.Credentials, configured []serversource.Root,
) *Sources {
	roots := make(map[string]Root, len(configured))
	for _, configuredRoot := range configured {
		digest := credentials.ServerImportRootDigest(configuredRoot.ID, configuredRoot.Path)
		roots[configuredRoot.ID] = Root{
			ID:     configuredRoot.ID,
			Label:  configuredRoot.Label,
			path:   configuredRoot.Path,
			digest: hex.EncodeToString(digest[:]),
		}
	}
	return &Sources{blobs: blobs, roots: roots}
}

func (service *Sources) Select(ctx context.Context, rootID, path string) (pegasusimportmodel.SelectedRoot, error) {
	if err := ctx.Err(); err != nil {
		return pegasusimportmodel.SelectedRoot{}, fmt.Errorf("select Pegasus source: %w", err)
	}
	root, err := service.validateCreateRequest(pegasusimportmodel.CreateRequest{RootID: rootID, SourceRelativePath: path})
	if err != nil {
		return pegasusimportmodel.SelectedRoot{}, err
	}
	return pegasusimportmodel.SelectedRoot{ID: root.ID, Label: root.Label, Digest: root.digest}, nil
}

func (service *Sources) validateCreateRequest(request pegasusimportmodel.CreateRequest) (Root, error) {
	if err := serversource.ValidateRootID(request.RootID); err != nil {
		return Root{}, fmt.Errorf("pegasusimport/validate root ID: %w", err)
	}
	root, ok := service.roots[request.RootID]
	if !ok {
		return Root{}, serversource.ErrRootNotFound
	}
	if err := serversource.ValidateRelativePath(request.SourceRelativePath); err != nil {
		return Root{}, fmt.Errorf("pegasusimport/validate source path: %w", err)
	}
	directory, err := serversource.OpenSelectedDirectory(root.path, request.SourceRelativePath)
	if err != nil {
		return Root{}, serversource.ErrRootUnavailable
	}
	cleanup.Error("close", directory.Close())
	return root, nil
}

func (service *Sources) OpenScan(unit pegasusimportmodel.Work) (application.ScannerSource, error) {
	root, err := service.workRoot(unit)
	if err != nil {
		return nil, err
	}
	return scanSource{root: root, selectedPath: unit.RelativePath, acquire: service.acquireSourceReader}, nil
}

func (service *Sources) OpenImport(unit pegasusimportmodel.Work) (application.ImportSources, error) {
	root, err := service.workRoot(unit)
	if err != nil {
		return nil, err
	}
	return importSource{service: service, root: root}, nil
}

func (service *Sources) workRoot(unit pegasusimportmodel.Work) (Root, error) {
	root, ok := service.roots[unit.RootID]
	if !ok || root.digest != unit.RootDigest {
		return Root{}, application.ErrRootChanged
	}
	return root, nil
}
