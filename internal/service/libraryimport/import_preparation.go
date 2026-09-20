package libraryimport

import (
	"context"
	"fmt"
	"io"

	blobmodel "retrom/internal/model/blob"
	"retrom/internal/model/diagnostics"
	model "retrom/internal/model/libraryimport"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/engine/rpgmaker/detector"
)

type ImportPreparation struct {
	projectArchives      model.ProjectArchiveOpener
	archiveInspector     model.ArchiveInspector
	diagnostics          diagnostics.ErrorReporter
	facts                model.ImportFactsReader
	catalog              model.ImportPreparationCatalog
	blobs                *blobstore.Store
	scummVMDetector      model.ScummVMDetector
	onsDetector          model.ONSProjectDetector
	butterscotchDetector model.ButterscotchProjectDetector
	nxengineDetector     model.NXEngineProjectDetector
	rpgMakerDetector     model.RPGMakerDetector
	options              ImportPreparationOptions
}

func NewImportPreparation(
	facts model.ImportFactsReader,
	catalog model.ImportPreparationCatalog,
	blobs *blobstore.Store,
	options ImportPreparationOptions,
) *ImportPreparation {
	return &ImportPreparation{
		projectArchives: options.ProjectArchives, archiveInspector: options.ArchiveInspector,
		diagnostics: options.Diagnostics,
		facts:       facts, catalog: catalog, blobs: blobs, options: options, scummVMDetector: options.ScummVMDetector,
		onsDetector: options.ONSDetector, butterscotchDetector: options.ButterscotchDetector,
		nxengineDetector: options.NXEngineDetector, rpgMakerDetector: options.RPGMakerDetector,
	}
}

func (service *ImportPreparation) Prepare(ctx context.Context, raw model.ImportRequest) (model.PreparedImport, error) {
	request, mode, err := NormalizeImportRequest(raw)
	if err != nil {
		return model.PreparedImport{}, err
	}
	facts, err := readAdmissionFacts(ctx, service.facts, request)
	if err != nil {
		return model.PreparedImport{}, err
	}
	request, mode, err = checkImportContent(request, mode, facts, ImportAdmissionOptions{
		MultiDiscEnabled:         service.options.MultiDiscEnabled,
		MetadataScraperAvailable: service.options.MetadataScraperAvailable,
	})
	if err != nil {
		return model.PreparedImport{}, err
	}
	plan := model.PreparedImport{
		Request: request, Upload: facts.Upload, Target: facts.Target, Files: facts.Files,
		ContentMode: mode, SourceType: facts.Upload.SourceType,
	}
	if err := service.activeDAT(ctx, &plan); err != nil {
		return model.PreparedImport{}, err
	}
	if err := service.prepareContent(ctx, &plan); err != nil {
		return model.PreparedImport{}, err
	}
	if err := service.resolveRPGTarget(ctx, &plan); err != nil {
		return model.PreparedImport{}, err
	}
	artifacts := NewImportArtifacts(preparationArtifactBlobs{store: service.blobs})
	plan.Groups, err = artifacts.Prepare(ctx, plan.Groups, plan.Archives)
	if err != nil {
		return model.PreparedImport{}, fmt.Errorf("prepare import artifacts: %w", err)
	}
	return plan, nil
}

func (service *ImportPreparation) activeDAT(ctx context.Context, plan *model.PreparedImport) error {
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

func (service *ImportPreparation) resolveRPGTarget(ctx context.Context, plan *model.PreparedImport) error {
	if plan.ContentMode != contentcapability.ModeRPGMakerProject {
		return nil
	}
	if len(plan.Groups) != 1 || plan.Groups[0].RPGProfile == nil {
		return model.ErrInvalid
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
		return nil, model.ErrInvalid
	}
	file, err := blobs.store.OpenDigest(digest)
	if err != nil {
		return nil, fmt.Errorf("open prepared artifact source: %w", err)
	}
	return file, nil
}

func (blobs preparationArtifactBlobs) Put(reader io.Reader) (blobmodel.PreparedBlob, error) {
	if blobs.store == nil {
		return blobmodel.PreparedBlob{}, model.ErrInvalid
	}
	result, err := blobs.store.Put(reader)
	if err != nil {
		return blobmodel.PreparedBlob{}, fmt.Errorf("store prepared artifact: %w", err)
	}
	return result, nil
}
