package libraryimport

import (
	"context"
	"fmt"
	"io"

	"retrom/internal/blobstore"
	"retrom/internal/contentcapability"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/scummvm"
)

type ImportPreparation struct {
	facts           ImportFactsReader
	catalog         ImportPreparationCatalog
	blobs           *blobstore.Store
	scummVMDetector *scummvm.Detector
	options         ImportPreparationOptions
}

func NewImportPreparation(
	facts ImportFactsReader, catalog ImportPreparationCatalog, blobs *blobstore.Store, options ImportPreparationOptions,
) *ImportPreparation {
	return &ImportPreparation{
		facts: facts, catalog: catalog, blobs: blobs, options: options, scummVMDetector: options.ScummVMDetector,
	}
}

func (service *ImportPreparation) Prepare(ctx context.Context, raw ImportRequest) (PreparedImport, error) {
	request, mode, err := NormalizeImportRequest(raw)
	if err != nil {
		return PreparedImport{}, err
	}
	facts, err := readAdmissionFacts(ctx, service.facts, request)
	if err != nil {
		return PreparedImport{}, err
	}
	request, mode, err = checkImportContent(request, mode, facts, ImportAdmissionOptions{
		MultiDiscEnabled:         service.options.MultiDiscEnabled,
		MetadataScraperAvailable: service.options.MetadataScraperAvailable,
	})
	if err != nil {
		return PreparedImport{}, err
	}
	plan := PreparedImport{
		Request: request, Upload: facts.Upload, Target: facts.Target, Files: facts.Files,
		ContentMode: mode, SourceType: facts.Upload.SourceType,
	}
	if err := service.activeDAT(ctx, &plan); err != nil {
		return PreparedImport{}, err
	}
	if err := service.prepareContent(ctx, &plan); err != nil {
		return PreparedImport{}, err
	}
	if err := service.resolveRPGTarget(ctx, &plan); err != nil {
		return PreparedImport{}, err
	}
	artifacts := NewImportArtifacts(preparationArtifactBlobs{store: service.blobs})
	plan.Groups, err = artifacts.Prepare(ctx, plan.Groups, plan.Archives)
	if err != nil {
		return PreparedImport{}, fmt.Errorf("prepare import artifacts: %w", err)
	}
	return plan, nil
}

func (service *ImportPreparation) activeDAT(ctx context.Context, plan *PreparedImport) error {
	if plan.Target.ProviderID == "" {
		return nil
	}
	datID, err := service.catalog.ActiveDAT(ctx, plan.Target.ProviderID, plan.Target.TargetID)
	if err != nil {
		return fmt.Errorf("read active import DAT: %w", err)
	}
	plan.DATVersionID = datID
	return nil
}

func (service *ImportPreparation) resolveRPGTarget(ctx context.Context, plan *PreparedImport) error {
	if plan.ContentMode != contentcapability.ModeRPGMakerProject {
		return nil
	}
	if len(plan.Groups) != 1 || plan.Groups[0].RPGProfile == nil {
		return ErrInvalid
	}
	target := plan.Target
	target.CoreID = detector.VirtualCoreID
	resolved, err := ResolveImportBinding(ctx, service.facts, target, string(plan.Groups[0].RPGProfile.ExpectedGeneration))
	if err != nil {
		return fmt.Errorf("resolve prepared RPG target: %w", err)
	}
	plan.Target = resolved
	return service.activeDAT(ctx, plan)
}

type preparationArtifactBlobs struct{ store *blobstore.Store }

func (blobs preparationArtifactBlobs) OpenDigest(digest string) (io.ReadCloser, error) {
	if blobs.store == nil {
		return nil, ErrInvalid
	}
	file, err := blobs.store.OpenDigest(digest)
	if err != nil {
		return nil, fmt.Errorf("open prepared artifact source: %w", err)
	}
	return file, nil
}

func (blobs preparationArtifactBlobs) Put(reader io.Reader) (blobstore.Metadata, error) {
	if blobs.store == nil {
		return blobstore.Metadata{}, ErrInvalid
	}
	result, err := blobs.store.Put(reader)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("store prepared artifact: %w", err)
	}
	return result, nil
}
