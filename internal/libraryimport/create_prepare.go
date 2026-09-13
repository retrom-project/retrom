package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"

	"retrom/internal/contentcapability"
	"retrom/internal/rpgmaker/detector"
)

type creationTarget = application.ImportTarget

type creationOptions struct {
	reviewHandoffKind string
	sourceCreation    *ownedSourceCreation
}

type creationPlan struct {
	reviewHandoffKind string
	sourceCreation    *ownedSourceCreation
	request           CreateRequest
	contentMode       string
	sourceType        string
	target            creationTarget
	datID             sql.NullString
	files             []importSourceFile
	dispositions      []preparedDisposition
	groups            []preparedGroup
	archives          []preparedArchive
}

func normalizeCreateRequest(request CreateRequest) (CreateRequest, string, error) {
	normalized, mode, err := application.NormalizeImportRequest(request)
	if err != nil {
		return CreateRequest{}, "", fmt.Errorf("normalize import request: %w", err)
	}
	return normalized, mode, nil
}

func (service *Service) prepareCreation(ctx context.Context, rawRequest CreateRequest) (creationPlan, error) {
	request, contentMode, err := normalizeCreateRequest(rawRequest)
	if err != nil {
		return creationPlan{}, err
	}
	purpose, sourceType, err := service.loadCompletedUpload(ctx, request.UploadID)
	if err != nil {
		return creationPlan{}, err
	}
	target, err := service.loadCreationTarget(ctx, request.TargetPlatformInstanceID)
	if err != nil {
		return creationPlan{}, err
	}
	files, err := service.loadImportSourceFiles(ctx, request.UploadID)
	if err != nil {
		return creationPlan{}, err
	}
	request, contentMode, err = normalizeTargetCreateRequest(
		request, contentMode, purpose, sourceType, files, target,
	)
	if err != nil {
		return creationPlan{}, err
	}
	if request.MetadataProvider == "HASHEOUS" && service.scraper == nil {
		return creationPlan{}, fmt.Errorf("libraryimport/service: %w", errMetadataScraperNotConfigured)
	}
	if err := validateCreationUpload(contentMode, sourceType, purpose); err != nil {
		return creationPlan{}, err
	}
	capabilities := contentcapability.Resolve(
		target.PlatformID, true, service.multiDiscImportEnabled, target.Policy,
	)
	if contentMode == contentcapability.ModeMultiDisc && capabilities.MultiDisc == nil {
		return creationPlan{}, ErrMultiDiscModeUnavailable
	}
	datID := sql.NullString{}
	if target.ProviderID != "" {
		datID = service.loadActiveDATID(ctx, target.ProviderID, target.TargetID)
	}
	plan := creationPlan{
		request: request, contentMode: contentMode, sourceType: sourceType, reviewHandoffKind: reviewHandoffDirect,
		target: target, datID: datID, files: files,
	}
	if err := service.prepareContent(ctx, &plan, capabilities); err != nil {
		return creationPlan{}, err
	}
	if err := service.resolveRPGMakerTarget(ctx, &plan); err != nil {
		return creationPlan{}, err
	}
	if contentMode == contentcapability.ModeRPGMakerProject {
		plan.datID = service.loadActiveDATID(ctx, plan.target.ProviderID, plan.target.TargetID)
	}
	return service.prepareCreationArtifacts(ctx, plan)
}

// A single archive selected through the ordinary file picker is still an RPG
// Maker project. The transport intent is deliberately normalized before the
// immutable queue snapshot is written, so every RPG generation follows the
// same detector and runtime-binding path as an explicit project upload.
func normalizeTargetCreateRequest(
	request CreateRequest, contentMode, purpose, sourceType string, files []importSourceFile, target creationTarget,
) (CreateRequest, string, error) {
	normalized, mode, err := application.NormalizeTargetImport(
		request, contentMode, purpose, sourceType, importFileFacts(files), importTargetFacts(target),
	)
	if err != nil {
		return CreateRequest{}, "", fmt.Errorf("normalize import target: %w", err)
	}
	return normalized, mode, nil
}

func normalizeTargetContentMode(platformID, contentMode string) string {
	return application.NormalizeTargetImportMode(platformID, contentMode)
}

func (service *Service) resolveRPGMakerTarget(ctx context.Context, plan *creationPlan) error {
	if plan.contentMode != contentcapability.ModeRPGMakerProject {
		return nil
	}
	if len(plan.groups) != 1 || plan.groups[0].RPGProfile == nil {
		return ErrInvalid
	}
	profile := plan.groups[0].RPGProfile
	return service.loadRPGTarget(ctx, &plan.target, profile.ExpectedGeneration)
}

func (service *Service) prepareContent(
	ctx context.Context,
	plan *creationPlan,
	capabilities contentcapability.ImportCapabilities,
) error {
	var err error
	switch plan.contentMode {
	case contentcapability.ModeMultiDisc:
		plan.dispositions, plan.groups, err = service.prepareMultiDiscFiles(plan.files, *capabilities.MultiDisc)
	case contentcapability.ModeRPGMakerProject:
		if plan.target.PlatformID != "rpgmaker" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareRPGMakerProject(
			ctx, plan.sourceType, plan.files, plan.target.DefaultCoreID,
		)
	case contentcapability.ModeONSProject, contentcapability.ModeKiriKiriProject, contentcapability.ModeNXEngineProject,
		contentcapability.ModeButterscotchProject, contentcapability.ModeTyranoScriptProject,
		contentcapability.ModeScummVMProject:
		return service.prepareEngineProject(ctx, plan)
	case contentcapability.ModeStandard:
		plan.dispositions, plan.groups, plan.archives = service.prepareImportFiles(
			ctx, plan.target.PlatformID, plan.sourceType, plan.files, plan.datID,
		)
	default:
		return ErrInvalid
	}
	return err
}

func validateCreationUpload(contentMode, sourceType, purpose string) error {
	if err := application.ValidateImportUpload(contentMode, sourceType, purpose); err != nil {
		return fmt.Errorf("validate import upload: %w", err)
	}
	return nil
}

func (service *Service) loadCompletedUpload(ctx context.Context, uploadID string) (string, string, error) {
	upload, found, err := repository.BindImportFacts(service.database).Upload(ctx, uploadID)
	if err != nil {
		return "", "", fmt.Errorf("read completed import upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" {
		return "", "", ErrInvalid
	}
	return upload.Purpose, upload.SourceType, nil
}

func (service *Service) loadCreationTarget(ctx context.Context, instanceID string) (creationTarget, error) {
	target, err := application.ReadImportTarget(ctx, repository.BindImportFacts(service.database), instanceID)
	if err != nil {
		return creationTarget{}, fmt.Errorf("read creation target: %w", err)
	}
	return legacyCreationTarget(target), nil
}

func (service *Service) loadRPGTarget(
	ctx context.Context,
	target *creationTarget,
	generation detector.Generation,
) error {
	target.CoreID = detector.VirtualCoreID
	return service.loadBoundTarget(ctx, target, string(generation))
}

func (service *Service) loadBoundTarget(ctx context.Context, target *creationTarget, detectorProfile string) error {
	resolved, err := application.ResolveImportBinding(
		ctx, repository.BindImportFacts(service.database), importTargetFacts(*target), detectorProfile,
	)
	if err != nil {
		return fmt.Errorf("read bound import target: %w", err)
	}
	*target = legacyCreationTarget(resolved)
	return nil
}

func (service *Service) loadActiveDATID(ctx context.Context, providerID, targetID string) sql.NullString {
	var datID sql.NullString
	_ = service.database.QueryRowContext(ctx, `
SELECT id FROM dat_versions WHERE provider_id=? AND target_id=? AND is_active=1
`, providerID, targetID).Scan(&datID)
	return datID
}

func (service *Service) loadImportSourceFiles(ctx context.Context, uploadID string) ([]importSourceFile, error) {
	files, err := repository.BindImportFacts(service.database).Files(ctx, uploadID)
	if err != nil {
		return nil, fmt.Errorf("read creation source files: %w", err)
	}
	if len(files) == 0 {
		return nil, ErrInvalid
	}
	return files, nil
}

func (service *Service) prepareEngineProject(ctx context.Context, plan *creationPlan) error {
	var err error
	switch plan.contentMode {
	case contentcapability.ModeONSProject:
		if plan.target.PlatformID != "ons" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareONSProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeKiriKiriProject:
		if plan.target.PlatformID != "kirikiri" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareKiriKiriProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeNXEngineProject:
		if plan.target.PlatformID != "cavestory" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareNXEngineProject(ctx, plan.sourceType, plan.files)
	case contentcapability.ModeButterscotchProject:
		if plan.target.PlatformID != "butterscotch" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareButterscotchProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeTyranoScriptProject:
		if plan.target.PlatformID != "tyranoscript" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareTyranoScriptProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeScummVMProject:
		if plan.target.PlatformID != "scummvm" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareScummVMProject(ctx, plan.sourceType, plan.files)
	default:
		return ErrInvalid
	}
	return err
}

func (service *Service) prepareCreationArtifacts(ctx context.Context, plan creationPlan) (creationPlan, error) {
	var err error
	artifacts := application.NewImportArtifacts(creationArtifactBlobs{store: service.blobs})
	plan.groups, err = artifacts.Prepare(ctx, plan.groups, plan.archives)
	if err != nil {
		return creationPlan{}, fmt.Errorf("prepare creation artifacts: %w", err)
	}
	return plan, nil
}
