package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/capability/content/contentcapability"
	contentcore "retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/engine/scummvm"
	corevalidationmodel "retrom/internal/model/corevalidation"
	application "retrom/internal/model/libraryimport"

	"github.com/google/uuid"
)

// ReviewDraftValidationResolver performs validation coordination from an
// immutable read port and returns only a persistence-neutral plan. It never
// receives an executor or a transaction.
type ReviewDraftValidationResolver struct {
	reader       application.ReviewValidationRefreshReader
	selected     application.ReviewValidationRefreshSelectedReader
	dependencies application.ReviewValidationRefreshDependencyReader
	now          func() time.Time
}

func NewReviewDraftValidationResolver(
	reader application.ReviewValidationRefreshReader,
	selected application.ReviewValidationRefreshSelectedReader,
	dependencies application.ReviewValidationRefreshDependencyReader,
	now func() time.Time,
) *ReviewDraftValidationResolver {
	if now == nil {
		now = time.Now
	}
	return &ReviewDraftValidationResolver{reader: reader, selected: selected, dependencies: dependencies, now: now}
}

func (resolver *ReviewDraftValidationResolver) Resolve(
	ctx context.Context, request ReviewDraftValidationRequest,
) (application.ReviewValidationPlan, error) {
	state, err := resolver.load(ctx, request.ItemID, request.TargetPlatformInstanceID, request.DefaultDOSEntry)
	if err != nil {
		return application.ReviewValidationPlan{}, err
	}
	state.rpgOverride = request.RPGSelfContainedOverride
	selected, current, err := state.loadExact()
	if err != nil {
		return application.ReviewValidationPlan{}, err
	}
	if current {
		return state.plan(selected), nil
	}
	if err := state.loadFallback(); err != nil {
		return application.ReviewValidationPlan{}, err
	}
	if state.sourceID == "" {
		return state.plan(""), nil
	}
	if err := state.resolveDependencies(); err != nil {
		return application.ReviewValidationPlan{}, err
	}
	return state.newValidationPlan()
}

// ResolveSelected validates an explicitly requested non-RPG validation against
// the current dependency facts. The selected record is a candidate, not a
// shortcut around dependency resolution.
func (resolver *ReviewDraftValidationResolver) ResolveSelected(
	ctx context.Context, request ReviewDraftSelectedValidationRequest,
) (application.ReviewValidationPlan, error) {
	if request.ValidationID == "" {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	if resolver.selected == nil {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	state, err := resolver.load(ctx, request.ItemID, request.TargetPlatformInstanceID, request.DefaultDOSEntry)
	if err != nil {
		return application.ReviewValidationPlan{}, err
	}
	record, found, err := resolver.selected.Selected(ctx, request.ItemID, request.ValidationID)
	if err != nil {
		return application.ReviewValidationPlan{}, fmt.Errorf("read selected review validation: %w", err)
	}
	if !found || !state.selectedRecordMatches(record) {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	state.setRecord(record)
	dependencyState, err := state.resolveDependencyState()
	if err != nil {
		return application.ReviewValidationPlan{}, err
	}
	state.dependencyState = dependencyState
	if state.sourceStatus != "READY" || !state.exactValidationCurrent() || !state.dependenciesCurrent() {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	return state.plan(record.ID), nil
}

func (resolver *ReviewDraftValidationResolver) SelectScummVM(
	ctx context.Context, request ReviewDraftScummVMRequest,
) (application.ReviewValidationPlan, error) {
	state, err := resolver.load(ctx, request.ItemID, request.TargetPlatformInstanceID, request.DefaultDOSEntry)
	if err != nil {
		return application.ReviewValidationPlan{}, err
	}
	if state.contentKind != scummvm.ContentKind || state.providerID != "retrom-runtime" ||
		state.runtimeTargetID != "scummvm" {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	_, current, err := state.loadExact()
	if err != nil {
		return application.ReviewValidationPlan{}, err
	}
	if !current || state.sourceID == "" {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	snapshot, err := scummvm.ParseSnapshot(state.dependencySnapshot)
	if err != nil || snapshot.Detection.SourceDigest != state.effectiveManifestDigest {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	selected, err := snapshot.Select(request.CandidateID)
	if err != nil {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	if selected.SelectedCandidateID == snapshot.SelectedCandidateID {
		return state.plan(state.sourceID), nil
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return application.ReviewValidationPlan{}, application.ErrInvalid
	}
	state.dependencySnapshot = string(encoded)
	state.sourceStatus, state.compatibilityCode = selected.Status()
	return state.newValidationPlan()
}

type draftValidationState struct {
	resolver *ReviewDraftValidationResolver
	ctx      context.Context
	itemID   string
	targetID string
	dosEntry *string

	draftID, effectiveSnapshotID, effectiveManifestDigest, contentKind string
	platformID, coreID, providerID, runtimeTargetID                    string
	platformVersion                                                    int64
	contentPolicy                                                      contentcapability.Policy
	dependencyFactsDigest                                              string
	datID                                                              *string
	sourceID, sourceManifestDigest, sourceInputDigest                  string
	sourceStatus, compatibilityCode, dependencySnapshot                string
	dependencyState                                                    draftDependencyState
	rpgOverride                                                        *bool
	rpgDependencyDigest                                                string
}

type draftDependencyState struct {
	tracked       bool
	replaceBundle bool
	snapshotJSON  string
	status        string
	code          string
	dependencies  []contentcore.BIOSDependency
}

func (resolver *ReviewDraftValidationResolver) load(
	ctx context.Context, itemID, targetID string, dosEntry *string,
) (*draftValidationState, error) {
	inputs, err := resolver.reader.Inputs(ctx, itemID, targetID)
	if err != nil {
		return nil, fmt.Errorf("read review validation inputs: %w", err)
	}
	return &draftValidationState{
		resolver: resolver, ctx: ctx, itemID: itemID, targetID: targetID, dosEntry: dosEntry,
		draftID: inputs.DraftID, effectiveSnapshotID: inputs.EffectiveSnapshotID,
		effectiveManifestDigest: inputs.EffectiveManifestDigest, contentKind: inputs.ContentKind,
		platformID: inputs.PlatformID, platformVersion: inputs.PlatformVersion, coreID: inputs.CoreID,
		providerID: inputs.ProviderID, runtimeTargetID: inputs.RuntimeTargetID,
		contentPolicy: inputs.ContentPolicy, dependencyFactsDigest: inputs.DependencyFactsDigest,
		datID: inputs.DATVersionID,
	}, nil
}

func (state *draftValidationState) loadExact() (string, bool, error) {
	record, found, err := state.resolver.reader.Exact(state.ctx, application.ReviewValidationRefreshLookup{
		ItemID: state.itemID, SourceSnapshotID: state.effectiveSnapshotID,
		TargetPlatformInstanceID: state.targetID, CoreID: state.coreID,
		ProviderID: state.providerID, TargetID: state.runtimeTargetID,
		DATVersionID: state.datID, DefaultDOSEntry: state.dosEntry,
	})
	if err != nil {
		return "", false, fmt.Errorf("libraryimport/review: %w", err)
	}
	if !found {
		return "", false, nil
	}
	state.setRecord(record)
	dependencyState, err := state.resolveDependencyState()
	if err != nil {
		return "", false, err
	}
	state.dependencyState = dependencyState
	if !state.exactValidationCurrent() {
		return "", false, nil
	}
	if !state.dependenciesCurrent() {
		return "", false, nil
	}
	if state.sourceStatus == "READY" {
		return state.sourceID, true, nil
	}
	return "", true, nil
}

func (state *draftValidationState) setRecord(record application.ReviewValidationRefreshRecord) {
	state.sourceID = record.ID
	state.sourceManifestDigest = record.SourceManifestDigest
	state.sourceInputDigest = record.PrepublishInputDigest
	state.sourceStatus = record.Status
	state.compatibilityCode = record.CompatibilityCode
	state.dependencySnapshot = record.DependencySnapshot
}

func (state *draftValidationState) selectedRecordMatches(
	record application.ReviewValidationRefreshRecord,
) bool {
	return record.ID != "" && record.SourceSnapshotID == state.effectiveSnapshotID &&
		record.TargetPlatformInstanceID == state.targetID && record.CoreID == state.coreID &&
		record.ProviderID == state.providerID && record.TargetID == state.runtimeTargetID &&
		record.SourceManifestDigest == state.effectiveManifestDigest &&
		sameValidationNullable(record.DATVersionID, state.datID) &&
		sameValidationNullable(record.DefaultDOSEntry, state.dosEntry)
}

func sameValidationNullable(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (state *draftValidationState) dependenciesCurrent() bool {
	return !state.dependencyState.tracked ||
		(state.dependencyState.snapshotJSON == state.dependencySnapshot &&
			state.dependencyState.status == state.sourceStatus &&
			state.dependencyState.code == state.compatibilityCode)
}

func (state *draftValidationState) exactValidationCurrent() bool {
	return application.PrepublishDigestMatches(state.sourceInputDigest, application.PrepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: state.effectiveSnapshotID,
		SourceManifestDigest: state.sourceManifestDigest, ContentKind: state.contentKind,
		TargetPlatformInstanceID: state.targetID, ProviderID: state.providerID,
		TargetID: state.runtimeTargetID, ContentPolicyDigest: state.contentPolicy.DigestFor(state.contentKind),
		DATVersionID: state.datID, DefaultDOSEntry: state.dosEntry,
		DependencySnapshot: json.RawMessage(state.dependencySnapshot), Status: state.sourceStatus,
		CompatibilityCode: state.compatibilityCode,
	})
}

func (state *draftValidationState) loadFallback() error {
	if state.sourceID != "" && (state.sourceStatus == "READY" || state.dependencyState.tracked) {
		return nil
	}
	record, found, err := state.resolver.reader.Fallback(state.ctx, application.ReviewValidationRefreshLookup{
		ItemID: state.itemID, SourceSnapshotID: state.effectiveSnapshotID,
		CoreID: state.coreID, ProviderID: state.providerID, TargetID: state.runtimeTargetID,
		DATVersionID: state.datID,
	})
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if !found {
		state.sourceID = ""
		return nil
	}
	state.setRecord(record)
	return nil
}

func (state *draftValidationState) resolveDependencyState() (draftDependencyState, error) {
	if state.contentKind == scummvm.ContentKind {
		snapshot, err := scummvm.ParseSnapshot(state.dependencySnapshot)
		if err != nil || snapshot.Detection.SourceDigest != state.effectiveManifestDigest {
			return draftDependencyState{}, application.ErrInvalid
		}
		status, code := snapshot.Status()
		return draftDependencyState{tracked: true, status: status, code: code, snapshotJSON: state.dependencySnapshot}, nil
	}
	if state.contentKind == "RPG_MAKER_PROJECT" {
		return state.resolveRPGDependencyState()
	}
	if isStaticBIOSSnapshot(state.dependencySnapshot) {
		return state.resolveStaticBIOSDependencyState()
	}
	return state.resolveArcadeDependencyState()
}

func (state *draftValidationState) resolveRPGDependencyState() (draftDependencyState, error) {
	profile, err := state.resolver.reader.RPGProfile(state.ctx, state.draftID)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: read RPG profile: %w", err)
	}
	if state.rpgOverride != nil {
		profile.SelfContainedOverride = *state.rpgOverride
	}
	dependencies, err := application.ResolveRPGReviewDependencies(profile)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("%w", err)
	}
	state.rpgDependencyDigest = dependencies.Digest
	return draftDependencyState{
		tracked: true, status: dependencies.Status, code: dependencies.Code,
		snapshotJSON: dependencies.SnapshotJSON,
	}, nil
}

func (state *draftValidationState) resolveStaticBIOSDependencyState() (draftDependencyState, error) {
	logicalName, err := state.resolver.reader.ContentLogicalName(state.ctx, state.effectiveSnapshotID)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	records, err := state.resolver.dependencies.BIOS(state.ctx, state.providerID, state.runtimeTargetID)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: read BIOS: %w", err)
	}
	snapshot, status, code, err := corevalidationmodel.ResolveBIOSRecords(records, logicalName)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return draftDependencyState{
		tracked: true, replaceBundle: true, snapshotJSON: string(encoded),
		status: status, code: code, dependencies: snapshot.BIOS,
	}, nil
}

func (state *draftValidationState) resolveArcadeDependencyState() (draftDependencyState, error) {
	resolved, err := ResolveCreationArcade(state.ctx, creationArcadeReader{reader: state.resolver.dependencies},
		state.providerID, state.runtimeTargetID, state.dependencySnapshot, state.sourceStatus, state.compatibilityCode)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("resolve arcade BIOS: %w", err)
	}
	return draftDependencyState{
		tracked: resolved.Tracked, status: resolved.Status, code: resolved.Code,
		snapshotJSON: resolved.SnapshotJSON, dependencies: resolved.Dependencies,
	}, nil
}

func (state *draftValidationState) resolveDependencies() error {
	dependencyState, err := state.resolveDependencyState()
	if err != nil {
		return err
	}
	state.dependencyState = dependencyState
	if dependencyState.tracked {
		state.sourceStatus, state.compatibilityCode, state.dependencySnapshot = dependencyState.status,
			dependencyState.code, dependencyState.snapshotJSON
	}
	return nil
}

func (state *draftValidationState) plan(selected string) application.ReviewValidationPlan {
	plan := application.ReviewValidationPlan{
		SelectedValidationID: selected, SelectedValidationPrepublishDigest: state.sourceInputDigest,
		Guard: state.guard(),
	}
	if state.contentKind == "RPG_MAKER_PROJECT" && state.dependencyState.tracked {
		plan.RPGDependencyDigest = state.rpgDependencyDigest
	}
	return plan
}

func (state *draftValidationState) guard() application.ReviewValidationGuard {
	return application.ReviewValidationGuard{
		SourceSnapshotID: state.effectiveSnapshotID, SourceManifestDigest: state.effectiveManifestDigest,
		ContentKind: state.contentKind, TargetPlatformInstanceID: state.targetID,
		PlatformInstanceVersion: state.platformVersion, PlatformID: state.platformID,
		CoreID: state.coreID, ProviderID: state.providerID, TargetID: state.runtimeTargetID,
		DATVersionID: copyString(state.datID), DefaultDOSEntry: copyString(state.dosEntry),
		ContentPolicyDigest:   state.contentPolicy.DigestFor(state.contentKind),
		DependencyFactsDigest: state.dependencyFactsDigest,
	}
}

func (state *draftValidationState) newValidationPlan() (application.ReviewValidationPlan, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return application.ReviewValidationPlan{}, fmt.Errorf("create review validation identity: %w", err)
	}
	createdAt := state.resolver.now().UnixMilli()
	create := &application.ReviewValidationRefreshCreate{
		ID: id.String(), ItemID: state.itemID, TargetPlatformInstanceID: state.targetID,
		PlatformInstanceVersion: state.platformVersion, CoreID: state.coreID,
		ProviderID: state.providerID, TargetID: state.runtimeTargetID, DATVersionID: state.datID,
		DefaultDOSEntry: state.dosEntry, SourceManifestDigest: state.effectiveManifestDigest,
		SourceSnapshotID: state.effectiveSnapshotID,
		PrepublishInputDigest: application.PrepublishDigest(application.PrepublishDigestInput{
			SchemaVersion:            1,
			SourceSnapshotID:         state.effectiveSnapshotID,
			SourceManifestDigest:     state.effectiveManifestDigest,
			ContentKind:              state.contentKind,
			TargetPlatformInstanceID: state.targetID,
			ProviderID:               state.providerID,
			TargetID:                 state.runtimeTargetID,
			ContentPolicyDigest:      state.contentPolicy.DigestFor(state.contentKind),
			DATVersionID:             state.datID,
			DefaultDOSEntry:          state.dosEntry,
			DependencySnapshot:       json.RawMessage(state.dependencySnapshot),
			Status:                   state.sourceStatus,
			CompatibilityCode:        state.compatibilityCode,
		}),
		Status: state.sourceStatus, CompatibilityCode: state.compatibilityCode,
		DependencySnapshotJSON: state.dependencySnapshot, CreatedAtMS: createdAt,
	}
	copyFiles := &application.ReviewValidationRefreshFileCopy{
		ValidationID: id.String(), SourceValidationID: state.sourceID, CreatedAtMS: createdAt,
		ReplaceBIOSBundle: state.dependencyState.replaceBundle, Dependencies: state.dependencyState.dependencies,
	}
	plan := application.ReviewValidationPlan{
		Create: create, Copy: copyFiles, SelectedValidationPrepublishDigest: create.PrepublishInputDigest,
		Guard: state.guard(),
	}
	if state.sourceStatus == "READY" {
		plan.SelectedValidationID = id.String()
	}
	if state.contentKind == "RPG_MAKER_PROJECT" && state.dependencyState.tracked {
		plan.RPGDependencyDigest = state.rpgDependencyDigest
	}
	return plan, nil
}

func isStaticBIOSSnapshot(raw string) bool {
	_, err := contentcore.ParseSnapshot(raw)
	return err == nil
}

type creationArcadeReader struct {
	reader application.ReviewValidationRefreshDependencyReader
}

func (reader creationArcadeReader) BIOS(
	ctx context.Context, providerID, targetID, logicalName string,
) (contentcore.BIOSDependency, bool, error) {
	dependency, found, err := reader.reader.ArcadeBIOS(ctx, providerID, targetID, logicalName)
	if err != nil {
		return contentcore.BIOSDependency{}, false, fmt.Errorf("read arcade BIOS dependency: %w", err)
	}
	return dependency, found, nil
}
