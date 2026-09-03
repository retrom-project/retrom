package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/rpgmaker/detector"
)

type creationTarget struct {
	platformID            string
	defaultCoreID         string
	coreID                string
	bindingID             string
	providerID            string
	targetID              string
	targetContractSHA256  string
	gameCompatibilityLine string
	deliveryProfile       string
	contentPolicyJSON     string
	instanceVersion       int64
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
	if projectCreateContentMode(contentMode) {
		request.MetadataProvider = "NONE"
	}
	return request, contentMode, nil
}

func validCreateContentMode(contentMode string) bool {
	switch contentMode {
	case contentcapability.ModeStandard, contentcapability.ModeMultiDiscM3UV1,
		contentcapability.ModeRPGMakerProjectV1, contentcapability.ModeONSProjectV1,
		contentcapability.ModeKiriKiriProjectV1, contentcapability.ModeButterscotchProjectV1,
		contentcapability.ModeTyranoScriptProjectV1:
		return true
	default:
		return false
	}
}

func projectCreateContentMode(contentMode string) bool {
	switch contentMode {
	case contentcapability.ModeRPGMakerProjectV1, contentcapability.ModeONSProjectV1,
		contentcapability.ModeKiriKiriProjectV1, contentcapability.ModeButterscotchProjectV1,
		contentcapability.ModeTyranoScriptProjectV1:
		return true
	default:
		return false
	}
}

func (service *Service) prepareCreation(ctx context.Context, rawRequest CreateRequest) (creationPlan, error) {
	request, contentMode, err := normalizeCreateRequest(rawRequest)
	if err != nil {
		return creationPlan{}, err
	}
	if request.MetadataProvider == "HASHEOUS" && service.scraper == nil {
		return creationPlan{}, fmt.Errorf("libraryimport/service: %w", errMetadataScraperNotConfigured)
	}
	purpose, sourceType, err := service.loadCompletedUpload(ctx, request.UploadID)
	if err != nil {
		return creationPlan{}, err
	}
	if err := validateCreationUpload(contentMode, sourceType, purpose); err != nil {
		return creationPlan{}, err
	}
	target, err := service.loadCreationTarget(ctx, request.TargetPlatformInstanceID)
	if err != nil {
		return creationPlan{}, err
	}
	capabilities := contentcapability.Resolve(
		target.platformID, true, service.multiDiscImportEnabled, target.contentPolicyJSON,
	)
	if contentMode == contentcapability.ModeMultiDiscM3UV1 && capabilities.MultiDisc == nil {
		return creationPlan{}, ErrMultiDiscModeUnavailable
	}
	files, err := service.loadImportSourceFiles(ctx, request.UploadID)
	if err != nil {
		return creationPlan{}, err
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
	if contentMode == contentcapability.ModeRPGMakerProjectV1 {
		plan.datID = service.loadActiveDATID(ctx, plan.target.providerID, plan.target.targetID)
	}
	return plan, nil
}

func (service *Service) resolveRPGMakerTarget(ctx context.Context, plan *creationPlan) error {
	if plan.contentMode != contentcapability.ModeRPGMakerProjectV1 {
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
	case contentcapability.ModeMultiDiscM3UV1:
		plan.dispositions, plan.groups, err = service.prepareMultiDiscFiles(plan.files, *capabilities.MultiDisc)
	case contentcapability.ModeRPGMakerProjectV1:
		if plan.target.platformID != "rpgmaker" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareRPGMakerProject(
			ctx, plan.sourceType, plan.files, plan.target.defaultCoreID,
		)
	case contentcapability.ModeONSProjectV1:
		if plan.target.platformID != "ons" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareONSProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeKiriKiriProjectV1:
		if plan.target.platformID != "kirikiri" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareKiriKiriProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeButterscotchProjectV1:
		if plan.target.platformID != "butterscotch" {
			return ErrInvalid
		}
		plan.dispositions, plan.groups, plan.archives, err = service.prepareButterscotchProject(
			ctx, plan.sourceType, plan.files,
		)
	case contentcapability.ModeTyranoScriptProjectV1:
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
	if contentMode == contentcapability.ModeMultiDiscM3UV1 && sourceType != "DIRECTORY" {
		return ErrMultiDiscModeUnavailable
	}
	if contentMode == contentcapability.ModeRPGMakerProjectV1 {
		if purpose != "RPG_MAKER_PROJECT" {
			return ErrInvalid
		}
		return nil
	}
	if contentMode == contentcapability.ModeONSProjectV1 {
		if purpose != "ONS_PROJECT" {
			return ErrInvalid
		}
		return nil
	}
	if contentMode == contentcapability.ModeKiriKiriProjectV1 {
		if purpose != "KIRIKIRI_PROJECT" {
			return ErrInvalid
		}
		return nil
	}
	if contentMode == contentcapability.ModeButterscotchProjectV1 {
		if purpose != "BUTTERSCOTCH_PROJECT" {
			return ErrInvalid
		}
		return nil
	}
	if contentMode == contentcapability.ModeTyranoScriptProjectV1 {
		if purpose != "TYRANOSCRIPT_PROJECT" {
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
 target.target_contract_sha256,target.game_compatibility_line,binding.delivery_profile
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
		&target.targetContractSHA256, &target.gameCompatibilityLine, &target.deliveryProfile,
	)
	if err != nil {
		return ErrInvalid
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT content_kind FROM runtime_binding_content_kinds WHERE binding_id=? ORDER BY content_kind
`, target.bindingID)
	if err != nil {
		return ErrInvalid
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	contentKinds := make([]string, 0, 2)
	for rows.Next() {
		var contentKind string
		if rows.Scan(&contentKind) != nil {
			return ErrInvalid
		}
		contentKinds = append(contentKinds, contentKind)
	}
	if rows.Err() != nil || len(contentKinds) == 0 {
		return ErrInvalid
	}
	policy := map[string]any{"schemaVersion": 1, "supportedContentKinds": contentKinds, "multiDisc": nil}
	for _, contentKind := range contentKinds {
		if contentKind == contentcapability.ModeMultiDiscM3UV1 {
			policy["multiDisc"] = map[string]any{
				"maxDiscs":      contentcapability.MaximumMultiDiscCount,
				"maxTotalBytes": contentcapability.MaximumMultiDiscBytes,
				"delivery":      contentcapability.DeliveryEagerExternal,
			}
		}
	}
	contents, err := json.Marshal(policy)
	if err != nil {
		return ErrInvalid
	}
	target.contentPolicyJSON = string(contents)
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
