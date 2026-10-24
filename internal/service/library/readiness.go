package library

import (
	"context"
	"errors"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

type biosProjection struct {
	Requirements      []runtimeclient.BiosRequirement `json:"biosRequirements"`
	Error             *string                         `json:"error"`
	CRCWarnings       []model.ReviewCRCWarning        `json:"crcWarnings"`
	MissingROMMembers []model.ReviewMissingROMMember  `json:"missingRomMembers"`
}

// Readiness reads current BIOS requirements; publishing remains a separate CAS operation.
func (s *Service) Readiness(ctx context.Context, p model.Principal, ids []string, coreID string) (
	model.List[model.ReviewReadiness], error,
) {
	result := model.List[model.ReviewReadiness]{Items: make([]model.ReviewReadiness, 0, len(ids))}
	if err := p.Admin(); err != nil {
		return result, wrap(err)
	}
	if err := readinessIDs(ids); err != nil {
		return result, err
	}
	installed, err := s.Repository.BiosFiles(ctx)
	if err != nil {
		return result, wrap(err)
	}
	biosFiles := make([]map[string]string, 0, len(installed))
	for _, file := range installed {
		if s.Runtime.Locate == nil {
			return result, wrap(model.ErrInvalid)
		}
		location, locateErr := s.Runtime.Locate(file.StorageKey)
		if locateErr != nil {
			return result, wrap(locateErr)
		}
		biosFiles = append(biosFiles, map[string]string{
			"requirementKey": file.RequirementKey, "filename": file.Filename, "path": location,
		})
	}
	providerRoots := make(map[string]string, len(s.Runtime.Providers))
	for id, provider := range s.Runtime.Providers {
		providerRoots[id] = provider.Root
	}
	inputs, positions := make([]map[string]any, 0, len(ids)), make([]int, 0, len(ids))
	for _, id := range ids {
		item, input := s.readinessInput(ctx, p, id)
		if input != nil {
			if coreID != "" {
				input["coreId"] = coreID
			}
			input["biosFiles"] = biosFiles
			input["providerRoots"] = providerRoots
			inputs = append(inputs, input)
			positions = append(positions, len(result.Items))
		}
		result.Items = append(result.Items, item)
	}
	if len(inputs) == 0 {
		return result, nil
	}
	var projections []biosProjection
	err = s.Runtime.Call(ctx, "batch-content-bios-requirements", map[string]any{"items": inputs}, &projections)
	if err != nil {
		return readinessUnavailable(ctx, result, positions)
	}
	unusable, err := s.unusableBIOS(ctx, projections, installed)
	if err != nil {
		return readinessUnavailable(ctx, result, positions)
	}
	applyBIOSProjections(result.Items, positions, projections, installed, unusable)
	return result, nil
}

// readinessUnavailable reports a failed runtime inspection on every inspected item, unless the caller left.
func readinessUnavailable(ctx context.Context, result model.List[model.ReviewReadiness], positions []int) (
	model.List[model.ReviewReadiness], error,
) {
	if ctx.Err() != nil {
		return result, wrap(ctx.Err())
	}
	for _, position := range positions {
		result.Items[position].Error = readinessFailure(false)
	}
	return result, nil
}

// unusableBIOS verifies each installed required archive whose layout the runtime declares, once per call.
func (s *Service) unusableBIOS(ctx context.Context, projections []biosProjection, installed []model.BiosFile,
) (map[string]bool, error) {
	files := make(map[string]model.BiosFile, len(installed))
	for _, file := range installed {
		files[file.RequirementKey] = file
	}
	unusable := make(map[string]bool)
	for _, projection := range projections {
		for _, requirement := range projection.Requirements {
			file, exists := files[requirement.RequirementKey]
			if _, checked := unusable[requirement.RequirementKey]; checked || !exists || !requirement.Required ||
				len(requirement.ArchiveEntries) == 0 {
				continue
			}
			location, err := s.Runtime.Locate(file.StorageKey)
			if err != nil {
				return nil, wrap(err)
			}
			verification, err := s.Runtime.VerifyBios(ctx, requirement, location)
			if err != nil {
				return nil, wrap(err)
			}
			unusable[requirement.RequirementKey] = verification.Status != "valid"
		}
	}
	return unusable, nil
}

func readinessIDs(ids []string) error {
	if len(ids) == 0 || len(ids) > 100 {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !model.UUID(id) || seen[id] {
			return model.ErrInvalid
		}
		seen[id] = true
	}
	return nil
}

func (s *Service) readinessInput(ctx context.Context, p model.Principal, id string) (
	model.ReviewReadiness, map[string]any,
) {
	item := model.ReviewReadiness{
		ID: id, MissingBIOS: []model.ReviewMissingBIOS{}, CRCWarnings: []model.ReviewCRCWarning{},
		MissingROMMembers: []model.ReviewMissingROMMember{},
	}
	detail, err := s.Repository.GameRuntimeDetail(ctx, p.User.ID, id, "pending_review")
	if err != nil {
		item.Error = readinessFailure(errors.Is(err, model.ErrNotFound))
		return item, nil
	}
	item.Version = &detail.Game.Version
	locators := make(map[string]runtimeclient.Locator, len(detail.ContentFiles))
	for _, file := range detail.ContentFiles {
		if s.Runtime.Locate == nil {
			item.Error = readinessFailure(false)
			return item, nil
		}
		location, locateErr := s.Runtime.Locate(file.StorageKey)
		if locateErr != nil {
			item.Error = readinessFailure(false)
			return item, nil
		}
		locators[file.LogicalKey] = runtimeclient.Locator{Path: location, Offset: file.Offset, SizeBytes: file.SizeBytes}
	}
	return item, map[string]any{
		"directory": map[string]any{
			"platformId": detail.Game.PlatformID, "defaultCoreId": detail.DefaultCoreID, "allowedCoreIds": detail.CoreIDs,
		},
		"config": detail.RuntimeConfig, "files": runtimeclient.Files(detail.ContentFiles),
		"locators": locators,
	}
}

func applyBIOSProjections(items []model.ReviewReadiness, positions []int,
	projections []biosProjection, installed []model.BiosFile, unusable map[string]bool,
) {
	keys := make(map[string]bool, len(installed))
	for _, file := range installed {
		keys[file.RequirementKey] = true
	}
	for index, position := range positions {
		item := &items[position]
		item.MissingBIOS = []model.ReviewMissingBIOS{}
		item.CRCWarnings = []model.ReviewCRCWarning{}
		item.MissingROMMembers = []model.ReviewMissingROMMember{}
		item.BIOSSatisfied, item.Error = nil, nil
		if len(projections) != len(positions) {
			item.Error = readinessFailure(false)
			continue
		}
		if projections[index].Error != nil {
			item.Error = readinessFailure(false)
			if failure := runtimeclient.ContentFailure(*projections[index].Error); failure != nil {
				item.Error = &model.ReviewReadinessError{Code: failure.Code, Message: failure.Message}
			}
			continue
		}
		if projections[index].Requirements == nil || projections[index].CRCWarnings == nil ||
			projections[index].MissingROMMembers == nil {
			item.Error = readinessFailure(false)
			continue
		}
		item.CRCWarnings = projections[index].CRCWarnings
		item.MissingROMMembers = projections[index].MissingROMMembers
		satisfied := true
		for _, member := range item.MissingROMMembers {
			if member.Role == "BIOS" {
				satisfied = false
			}
		}
		for _, requirement := range projections[index].Requirements {
			if requirement.Required && (!keys[requirement.RequirementKey] || unusable[requirement.RequirementKey]) {
				satisfied = false
				item.MissingBIOS = append(item.MissingBIOS, model.ReviewMissingBIOS{
					Key: requirement.RequirementKey, Name: requirement.LogicalName, CoreID: requirement.CoreID,
				})
			}
		}
		item.BIOSSatisfied = &satisfied
	}
}

func readinessFailure(unavailable bool) *model.ReviewReadinessError {
	if unavailable {
		return &model.ReviewReadinessError{Code: "REVIEW_UNAVAILABLE", Message: "条目已不在待审核列表，请刷新后重试。"}
	}
	return &model.ReviewReadinessError{
		Code: "REVIEW_READINESS_UNAVAILABLE", Message: "无法检查运行条件，请稍后重新检查；若持续失败，请检查运行配置和游戏文件。",
	}
}
