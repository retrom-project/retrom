package libraryimport

import (
	"context"
	"fmt"
	"slices"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/security/authn"
	taggingmodel "retrom/internal/model/tagging"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/service/payloadrelease"
	taggingservice "retrom/internal/service/tagging"
)

type ImportCreations struct {
	repository  model.ImportCreationRepository
	preparation *ImportPreparation
	tags        *taggingservice.Service
	scraper     *metadatascrape.Service
	settings    ImportCreationSettings
	newID       func() (string, error)
}

func NewImportCreations(repository model.ImportCreationRepository, preparation *ImportPreparation,
	tags *taggingservice.Service, scraper *metadatascrape.Service, settings ImportCreationSettings,
) *ImportCreations {
	if settings.Now == nil {
		settings.Now = time.Now
	}
	return &ImportCreations{
		repository:  repository,
		preparation: preparation,
		tags:        tags,
		scraper:     scraper,
		settings:    settings,
		newID:       newImportAdmissionID,
	}
}

func (service *ImportCreations) Create(ctx context.Context, request model.ImportRequest,
	options model.ImportCreationOptions,
) (model.ImportCreationResult, error) {
	plan, err := service.preparation.Prepare(ctx, request)
	if err != nil {
		return model.ImportCreationResult{}, fmt.Errorf("prepare creation: %w", err)
	}
	return service.CommitPrepared(ctx, plan, options)
}

func (service *ImportCreations) CommitPrepared(ctx context.Context, plan model.PreparedImport,
	options model.ImportCreationOptions,
) (model.ImportCreationResult, error) {
	run, err := service.prepareCommit(plan, options)
	if err != nil {
		return model.ImportCreationResult{}, creationError("commit prepared", err)
	}
	err = service.repository.WithCreation(
		ctx,
		func(scope model.ImportCreationScope) error { return run.commit(ctx, scope) },
	)
	if err != nil {
		return model.ImportCreationResult{}, fmt.Errorf("commit import creation: %w", err)
	}
	for _, scheduled := range run.scheduled {
		if !scheduled.IsNoop() {
			service.scraper.Dispatch(ctx, scheduled.ScrapeRunID())
		}
	}
	return run.result, nil
}

type creationCommit struct {
	service                       *ImportCreations
	plan                          model.PreparedImport
	options                       model.ImportCreationOptions
	header                        model.CreationHeader
	groups                        []creationGroup
	result                        model.ImportCreationResult
	sourceBefore                  model.SourceCreationSnapshot
	tags                          []taggingmodel.Reference
	actor                         authn.Actor
	actorID                       string
	scheduled                     []metadatascrape.Scheduled
	materialized                  map[string]map[int]string
	sourceCounts, duplicateCounts map[string]int64
	duplicates                    int64
	parentVersion                 int64
}
type creationGroup struct {
	group                                                                                   model.PreparedGroup
	itemID, snapshotID, validationID, draftID, kind, groupKey, manifestJSON, manifestDigest string
	searchParts                                                                             []string
	uploadIDs                                                                               []string
}

func (service *ImportCreations) prepareCommit(
	plan model.PreparedImport,
	options model.ImportCreationOptions,
) (*creationCommit, error) {
	options, err := normalizeCreationOptions(plan, options)
	if err != nil {
		return nil, err
	}
	run := &creationCommit{
		service:         service,
		plan:            plan,
		options:         options,
		materialized:    map[string]map[int]string{},
		sourceCounts:    map[string]int64{},
		duplicateCounts: map[string]int64{},
	}
	if options.Queued != nil {
		run.header.ImportID, run.header.JobID = options.Queued.ImportID, options.Queued.JobID
	} else {
		for _, destination := range []*string{&run.header.ImportID, &run.header.JobID, &run.header.ConsumptionID} {
			if err := service.allocate(destination); err != nil {
				return nil, creationError("prepare commit", err)
			}
		}
	}
	for _, group := range plan.Groups {
		record, err := service.prepareGroup(group, plan.Archives)
		if err != nil {
			return nil, creationError("prepare commit", err)
		}
		run.groups = append(run.groups, record)
	}
	return run, nil
}

func (service *ImportCreations) allocate(destination *string) error {
	id, err := service.newID()
	if err != nil {
		return fmt.Errorf("allocate creation identity: %w", err)
	}
	if id == "" {
		return model.ErrInvalid
	}
	*destination = id
	return nil
}

func (run *creationCommit) commit(ctx context.Context, scope model.ImportCreationScope) error {
	if err := run.checkInputs(ctx, scope); err != nil {
		return creationError("commit", err)
	}
	if err := run.initialize(ctx, scope); err != nil {
		return creationError("commit", err)
	}
	for _, archive := range run.plan.Archives {
		materialized, err := scope.Sources.Archive(ctx, archive, run.header.NowMS)
		if err != nil {
			return fmt.Errorf("catalog prepared archive: %w", err)
		}
		run.materialized[archive.BlobID] = materialized
	}
	for index := range run.groups {
		if err := run.persistGroup(ctx, scope, &run.groups[index]); err != nil {
			return fmt.Errorf("create group %d: %w", index, err)
		}
	}
	if err := run.complete(ctx, scope); err != nil {
		return creationError("commit", err)
	}
	return run.bindSource(ctx, scope)
}

func (run *creationCommit) initialize(ctx context.Context, scope model.ImportCreationScope) error {
	run.actor = authn.ActorFromContext(ctx, "release-setup")
	actorID, _ := run.actor.UserID.(string)
	if run.options.Queued != nil {
		actorID = run.options.Queued.ActorUserID
	}
	if len(run.plan.Request.TagIDs) > 0 && actorID == "" {
		return model.ErrInvalid
	}
	run.actorID = actorID
	var err error
	run.tags, err = scope.Tags.ValidateActiveReferences(ctx, run.plan.Request.TagIDs)
	if err != nil {
		return fmt.Errorf("validate creation tags: %w", err)
	}
	run.header.NowMS = run.service.settings.Now().UnixMilli()
	if err := run.prepareDependencies(ctx, scope); err != nil {
		return creationError("initialize", err)
	}
	if err := run.prepareHeader(ctx, scope); err != nil {
		return creationError("initialize", err)
	}
	if err := scope.Headers.FenceInputs(ctx, run.plan); err != nil {
		return fmt.Errorf("fence creation inputs: %w", err)
	}
	if err := scope.Headers.Header(ctx, run.header); err != nil {
		return fmt.Errorf("create import header: %w", err)
	}
	run.parentVersion = 1
	if run.header.Queued != nil {
		run.parentVersion = run.header.Queued.ParentVersion + 1
	}
	return nil
}

func (run *creationCommit) bindSource(ctx context.Context, scope model.ImportCreationScope) error {
	if run.options.Source == nil {
		return nil
	}
	result, err := scope.Results.Read(ctx, run.result.Created)
	if err != nil {
		return fmt.Errorf("read created owned source: %w", err)
	}
	if len(result.Items) != 1 {
		return model.ErrInvalid
	}
	if err := ValidateOwnedSourceGroups(
		run.options.Source.Intent.PrimaryPaths,
		[][]string{result.Items[0].SourceRelativePaths},
	); err != nil {
		return creationError("bind source", err)
	}
	if err := NewSourceOwnership(run.service.settings.Now).Attach(
		ctx,
		scope.Ownership,
		run.sourceBefore,
		result.Created,
		result.Items[0],
	); err != nil {
		return creationError("bind source", err)
	}
	run.result.Owned = result
	return nil
}

func validateOwnedImportPlan(plan model.PreparedImport, source *model.OwnedImportCreation) error {
	if source == nil {
		return nil
	}
	before, target := source.Before, plan.Target
	if before.TargetVersion != target.Version || before.TargetPlatformID != target.PlatformID ||
		before.TargetDefaultCoreID != target.DefaultCoreID {
		return model.ErrVersionConflict
	}
	if target.PlatformID != "rpgmaker" &&
		(before.TargetProviderID != target.ProviderID || before.TargetID != target.TargetID ||
			before.TargetDATVersionID != plan.DATVersionID) {
		return model.ErrVersionConflict
	}
	groups := make([][]string, 0, len(plan.Groups))
	for _, group := range plan.Groups {
		paths := []string{}
		for _, source := range group.Sources {
			if source.Role != "COMPANION" {
				paths = append(paths, source.File.Path)
			}
		}
		groups = append(groups, paths)
	}
	return ValidateOwnedSourceGroups(source.Intent.PrimaryPaths, groups)
}

func cloneCreationGroup(group model.PreparedGroup) model.PreparedGroup {
	group.ValidationFiles = slices.Clone(group.ValidationFiles)
	group.Sources = slices.Clone(group.Sources)
	return group
}

func (run *creationCommit) schedulePayload(ctx context.Context, scope model.ImportCreationScope) error {
	_, err := payloadrelease.NewScheduler(nil).TerminalImport(ctx, scope.Payload, run.header.ImportID, run.header.NowMS)
	if err != nil {
		return fmt.Errorf("schedule creation payload: %w", err)
	}
	return nil
}

func normalizeCreationOptions(
	plan model.PreparedImport,
	options model.ImportCreationOptions,
) (model.ImportCreationOptions, error) {
	if options.ReviewHandoffKind == "" {
		options.ReviewHandoffKind = "DIRECT"
	}
	if options.ReviewHandoffKind != "DIRECT" && options.ReviewHandoffKind != "EMULATIONSTATION" {
		return model.ImportCreationOptions{}, model.ErrInvalid
	}
	if options.Queued != nil && (options.Source != nil || options.Reconfiguration != nil) {
		return model.ImportCreationOptions{}, model.ErrInvalid
	}
	if plan.Upload.ID == "" || plan.Upload.ID != plan.Request.UploadID ||
		plan.Target.ID != plan.Request.TargetPlatformInstanceID {
		return model.ImportCreationOptions{}, model.ErrInvalid
	}
	if err := validateOwnedImportPlan(plan, options.Source); err != nil {
		return model.ImportCreationOptions{}, creationError("prepare commit", err)
	}
	return options, nil
}

func creationError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}
