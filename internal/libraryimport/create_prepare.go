package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/contentprofile"
	"retrom/internal/rpgmaker/detector"
)

type creationTarget struct {
	platformID      string
	defaultCoreID   string
	coreID          string
	bindingID       string
	providerID      string
	targetID        string
	deliveryProfile string
	contentPolicy   contentcapability.Policy
	instanceVersion int64
}

type creationPlan struct {
	request      CreateRequest
	contentMode  string
	sourceType   string
	target       creationTarget
	datID        sql.NullString
	files        []importSourceFile
	dispositions []preparedDisposition
	groups       []preparedGroup
	archives     []preparedArchive
}

func normalizeCreateRequest(request CreateRequest) (CreateRequest, string, error) {
	if request.TagIDs == nil {
		request.TagIDs = []string{}
	}
	if request.reviewHandoffKind == "" {
		request.reviewHandoffKind = reviewHandoffDirect
	}
	if request.reviewHandoffKind != reviewHandoffDirect &&
		request.reviewHandoffKind != reviewHandoffEmulationStation {
		return CreateRequest{}, "", ErrInvalid
	}
	contentMode := request.ContentMode
	if contentMode == "" {
		contentMode = contentcapability.ModeStandard
	}
	if !validCreateContentMode(contentMode) {
		return CreateRequest{}, "", ErrInvalid
	}
	if request.MetadataProvider != "NONE" && request.MetadataProvider != "HASHEOUS" {
		return CreateRequest{}, "", ErrInvalid
	}
	if contentcapability.IsProjectMode(contentMode) {
		request.MetadataProvider = "NONE"
	}
	return request, contentMode, nil
}

func validCreateContentMode(contentMode string) bool {
	return contentMode == contentcapability.ModeStandard || contentMode == contentcapability.ModeMultiDisc ||
		contentcapability.IsProjectMode(contentMode)
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
		target.platformID, true, service.multiDiscImportEnabled, target.contentPolicy,
	)
	if contentMode == contentcapability.ModeMultiDisc && capabilities.MultiDisc == nil {
		return creationPlan{}, ErrMultiDiscModeUnavailable
	}
	datID := sql.NullString{}
	if target.providerID != "" {
		datID = service.loadActiveDATID(ctx, target.providerID, target.targetID)
	}
	plan := creationPlan{
		request: request, contentMode: contentMode, sourceType: sourceType,
		target: target, datID: datID, files: files,
	}
	if err := service.prepareContent(ctx, &plan, capabilities); err != nil {
		return creationPlan{}, err
	}
	if err := service.resolveRPGMakerTarget(ctx, &plan); err != nil {
		return creationPlan{}, err
	}
	if contentMode == contentcapability.ModeRPGMakerProject {
		plan.datID = service.loadActiveDATID(ctx, plan.target.providerID, plan.target.targetID)
	}
	return plan, nil
}

// A single archive selected through the ordinary file picker is still an RPG
// Maker project. The transport intent is deliberately normalized before the
// immutable queue snapshot is written, so every RPG generation follows the
// same detector and runtime-binding path as an explicit project upload.
func normalizeTargetCreateRequest(
	request CreateRequest,
	contentMode, purpose, sourceType string,
	files []importSourceFile,
	target creationTarget,
) (CreateRequest, string, error) {
	if target.platformID != "rpgmaker" {
		return request, contentMode, nil
	}
	contentMode = normalizeTargetContentMode(target.platformID, contentMode)
	if contentMode == contentcapability.ModeRPGMakerProject {
		request.ContentMode = contentMode
		request.MetadataProvider = "NONE"
	}
	if contentMode != contentcapability.ModeRPGMakerProject || purpose != "GENERAL" {
		return request, contentMode, nil
	}
	if sourceType == "DIRECTORY" {
		return request, contentMode, nil
	}
	if sourceType != "FILES" || len(files) != 1 {
		return CreateRequest{}, "", ErrInvalid
	}
	format, reason := profileArchiveFormat(files[0].path)
	if reason != "" || format != contentprofile.ArchiveZIP && format != contentprofile.ArchiveSevenZip {
		return CreateRequest{}, "", ErrInvalid
	}
	return request, contentMode, nil
}

func normalizeTargetContentMode(platformID, contentMode string) string {
	if platformID == "rpgmaker" && contentMode == contentcapability.ModeStandard {
		return contentcapability.ModeRPGMakerProject
	}
	return contentMode
}

func (service *Service) resolveRPGMakerTarget(ctx context.Context, plan *creationPlan) error {
	if plan.contentMode != contentcapability.ModeRPGMakerProject {
		return nil
	}
	if len(plan.groups) != 1 || plan.groups[0].rpgProfile == nil {
		return ErrInvalid
	}
	profile := plan.groups[0].rpgProfile
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
		if plan.target.platformID != "rpgmaker" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareRPGMakerProject(
			ctx, plan.sourceType, plan.files, plan.target.defaultCoreID,
		)
	case contentcapability.ModeONSProject:
		if plan.target.platformID != "ons" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareONSProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeKiriKiriProject:
		if plan.target.platformID != "kirikiri" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareKiriKiriProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeButterscotchProject:
		if plan.target.platformID != "butterscotch" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareButterscotchProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeTyranoScriptProject:
		if plan.target.platformID != "tyranoscript" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareTyranoScriptProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeStandard:
		plan.dispositions, plan.groups, plan.archives = service.prepareImportFiles(
			ctx, plan.target.platformID, plan.sourceType, plan.files, plan.datID,
		)
	default:
		return ErrInvalid
	}
	return err
}

func validateCreationUpload(contentMode, sourceType, purpose string) error {
	if contentMode == contentcapability.ModeMultiDisc && sourceType != "DIRECTORY" {
		return ErrMultiDiscModeUnavailable
	}
	if contentcapability.IsProjectMode(contentMode) {
		if purpose != "PROJECT" && purpose != "GENERAL" {
			return ErrInvalid
		}
		return nil
	}
	if purpose != "GENERAL" {
		return ErrInvalid
	}
	return nil
}

func (service *Service) loadCompletedUpload(ctx context.Context, uploadID string) (string, string, error) {
	var state, purpose, sourceType string
	err := service.database.QueryRowContext(ctx, `
SELECT state,purpose,source_type
FROM upload_sessions
WHERE id=?
`, uploadID).Scan(&state, &purpose, &sourceType)
	if err != nil || state != "COMPLETE" {
		return "", "", ErrInvalid
	}
	return purpose, sourceType, nil
}

func (service *Service) loadCreationTarget(ctx context.Context, instanceID string) (creationTarget, error) {
	var target creationTarget
	err := service.database.QueryRowContext(ctx, `
SELECT pi.platform_id,pi.default_core_id,pi.version
FROM platform_instances pi
WHERE pi.id=? AND pi.enabled=1 AND pi.deleted_at_ms IS NULL
`, instanceID).Scan(
		&target.platformID, &target.defaultCoreID, &target.instanceVersion,
	)
	if err != nil {
		return creationTarget{}, ErrInvalid
	}
	target.coreID = target.defaultCoreID
	if target.platformID == "rpgmaker" && target.defaultCoreID == detector.VirtualCoreID {
		return target, nil
	}
	return target, service.loadBoundTarget(ctx, &target, "")
}

func (service *Service) loadRPGTarget(
	ctx context.Context,
	target *creationTarget,
	generation detector.Generation,
) error {
	target.coreID = detector.VirtualCoreID
	return service.loadBoundTarget(ctx, target, string(generation))
}

func (service *Service) loadBoundTarget(
	ctx context.Context,
	target *creationTarget,
	detectorProfile string,
) error {
	query := `
SELECT binding.binding_id,binding.core_id,binding.provider_id,binding.target_id,
 binding.delivery_profile,` + contentcapability.BindingPolicySQL + `
FROM runtime_target_bindings binding
JOIN runtime_binding_platforms platform ON platform.binding_id=binding.binding_id AND platform.platform_id=?
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE binding.core_id=? AND binding.launch_policy!='DISABLED'`
	arguments := []any{target.platformID, target.coreID}
	if detectorProfile != "" {
		query += ` AND binding.detector_profile=?`
		arguments = append(arguments, detectorProfile)
	}
	err := service.database.QueryRowContext(ctx, query, arguments...).Scan(
		&target.bindingID, &target.coreID, &target.providerID, &target.targetID,
		&target.deliveryProfile, &target.contentPolicy,
	)
	if err != nil {
		return ErrInvalid
	}
	if len(target.contentPolicy.SupportedContentKinds) == 0 {
		return ErrInvalid
	}
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
	rows, err := service.database.QueryContext(ctx, `
SELECT f.id,f.relative_path,f.final_blob_id,b.sha256,b.size_bytes
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.upload_session_id=? AND f.state='COMPLETE'
ORDER BY f.relative_path,f.id
`, uploadID)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var files []importSourceFile
	for rows.Next() {
		var file importSourceFile
		if err := rows.Scan(&file.id, &file.path, &file.blobID, &file.sha256, &file.size); err != nil {
			return nil, fmt.Errorf("libraryimport/service: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/service: %w", err)
	}
	if len(files) == 0 {
		return nil, ErrInvalid
	}
	return files, nil
}
