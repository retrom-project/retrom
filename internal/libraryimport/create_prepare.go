package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/routing"
)

type creationTarget struct {
	platformID          string
	defaultCoreID       string
	coreID              string
	artifactID          string
	emulatorVersion     string
	artifactPath        string
	artifactSHA         string
	artifactSetSHA      string
	routeKey            string
	runtimeFamily       string
	adapterID           string
	adapterABI          string
	compatibilityConfig string
	instanceVersion     int64
	artifactVersion     int64
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
		contentcapability.ModeKiriKiriProjectV1, contentcapability.ModeButterscotchProjectV1:
		return true
	default:
		return false
	}
}

func projectCreateContentMode(contentMode string) bool {
	switch contentMode {
	case contentcapability.ModeRPGMakerProjectV1, contentcapability.ModeONSProjectV1,
		contentcapability.ModeKiriKiriProjectV1, contentcapability.ModeButterscotchProjectV1:
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
		target.platformID, true, service.multiDiscImportEnabled, target.compatibilityConfig,
	)
	if contentMode == contentcapability.ModeMultiDiscM3UV1 && capabilities.MultiDisc == nil {
		return creationPlan{}, ErrMultiDiscModeUnavailable
	}
	files, err := service.loadImportSourceFiles(ctx, request.UploadID)
	if err != nil {
		return creationPlan{}, err
	}
	datID := sql.NullString{}
	if target.artifactID != "" {
		datID = service.loadActiveDATID(ctx, target.artifactID)
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
		plan.datID = service.loadActiveDATID(ctx, plan.target.artifactID)
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
	route, err := routing.Current(profile.SelectedCoreID, profile.ExpectedGeneration)
	if err != nil {
		return ErrInvalid
	}
	return service.loadArtifactTarget(ctx, &plan.target, route)
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
	return target, service.loadDefaultArtifactTarget(ctx, &target)
}

func (service *Service) loadDefaultArtifactTarget(ctx context.Context, target *creationTarget) error {
	var route routing.Entry
	err := service.database.QueryRowContext(ctx, `
SELECT id,runtime_version,entry_path,sha256,version,compatibility_json,
artifact_set_sha256,route_key,runtime_family,adapter_id
FROM core_artifacts
WHERE core_id=? AND selected_for_new_bindings=1
`, target.coreID).Scan(
		&target.artifactID, &target.emulatorVersion, &target.artifactPath, &target.artifactSHA,
		&target.artifactVersion, &target.compatibilityConfig, &target.artifactSetSHA,
		&target.routeKey, &target.runtimeFamily, &target.adapterID,
	)
	if err != nil {
		return ErrInvalid
	}
	return decodeTargetAdapterABI(target, route)
}

func (service *Service) loadArtifactTarget(
	ctx context.Context,
	target *creationTarget,
	route routing.Entry,
) error {
	target.coreID = route.CoreID
	err := service.database.QueryRowContext(ctx, `
SELECT id,runtime_version,entry_path,sha256,version,compatibility_json,
artifact_set_sha256,route_key,runtime_family,adapter_id
FROM core_artifacts
WHERE core_id=? AND route_key=? AND selected_for_new_bindings=1 AND available_for_launch=1
`, route.CoreID, route.RouteKey).Scan(
		&target.artifactID, &target.emulatorVersion, &target.artifactPath, &target.artifactSHA,
		&target.artifactVersion, &target.compatibilityConfig, &target.artifactSetSHA,
		&target.routeKey, &target.runtimeFamily, &target.adapterID,
	)
	if err != nil {
		return ErrInvalid
	}
	return decodeTargetAdapterABI(target, route)
}

func decodeTargetAdapterABI(target *creationTarget, route routing.Entry) error {
	var compatibility struct {
		AdapterABI string `json:"adapterAbi"`
	}
	if json.Unmarshal([]byte(target.compatibilityConfig), &compatibility) != nil || compatibility.AdapterABI == "" {
		return ErrInvalid
	}
	target.adapterABI = compatibility.AdapterABI
	if route.RouteKey != "" && (target.routeKey != route.RouteKey || target.adapterID != route.AdapterID ||
		target.adapterABI != route.AdapterABI) {
		return ErrInvalid
	}
	return nil
}

func (service *Service) loadActiveDATID(ctx context.Context, artifactID string) sql.NullString {
	var datID sql.NullString
	_ = service.database.QueryRowContext(ctx, `
SELECT id FROM dat_versions WHERE core_artifact_id=? AND is_active=1
`, artifactID).Scan(&datID)
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
