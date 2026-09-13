package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/dbexec"
	repository "retrom/internal/persistence/libraryimport"

	application "retrom/internal/service/libraryimport"

	validationpersistence "retrom/internal/persistence/corevalidation"
	validationservice "retrom/internal/service/corevalidation"

	"retrom/internal/capability/content/contentcapability"

	"retrom/internal/capability/content/corevalidation"

	"github.com/google/uuid"
)

// Keep immutable validation refresh branches together for auditability.
func (service *Service) ensureCompatibleDraftValidation(
	ctx context.Context,
	transaction dbexec.Executor,
	itemID, targetID string,
	dosEntry sql.NullString,
) (string, error) {
	state := draftValidationRefresh{
		service: service, ctx: ctx, transaction: transaction,
		itemID: itemID, targetID: targetID, dosEntry: dosEntry,
	}
	if err := state.loadInputs(); err != nil {
		return "", err
	}
	selected, current, err := state.loadExactValidation()
	if err != nil || current {
		return selected, err
	}
	if err := state.loadFallbackValidation(); err != nil {
		return "", err
	}
	if state.sourceID == "" {
		return "", nil
	}
	if err := state.resolveDependencies(); err != nil {
		return "", err
	}
	return state.insertValidation()
}

type draftValidationRefresh struct {
	service                 *Service
	ctx                     context.Context
	transaction             dbexec.Executor
	itemID                  string
	targetID                string
	draftID                 string
	dosEntry                sql.NullString
	effectiveSnapshotID     string
	effectiveManifestDigest string
	contentKind             string
	platformVersion         int64
	coreID                  string
	providerID              string
	runtimeTargetID         string
	contentPolicy           contentcapability.Policy
	datID                   sql.NullString
	sourceID                string
	sourceManifestDigest    string
	sourceInputDigest       string
	sourceStatus            string
	compatibilityCode       string
	dependencySnapshot      string
	dependencyState         draftDependencyState
}

func (state *draftValidationRefresh) loadInputs() error {
	inputs, err := repository.BindReviewValidation(state.transaction).Inputs(
		state.ctx, state.itemID, state.targetID,
	)
	if err != nil {
		return fmt.Errorf("read review validation inputs: %w", err)
	}
	state.draftID = inputs.DraftID
	state.effectiveSnapshotID = inputs.EffectiveSnapshotID
	state.effectiveManifestDigest = inputs.EffectiveManifestDigest
	state.contentKind = inputs.ContentKind
	state.platformVersion = inputs.PlatformVersion
	state.coreID = inputs.CoreID
	state.providerID = inputs.ProviderID
	state.runtimeTargetID = inputs.RuntimeTargetID
	state.datID = nullableCandidate(inputs.DATVersionID)
	state.contentPolicy = inputs.ContentPolicy
	return nil
}

func (state *draftValidationRefresh) loadExactValidation() (string, bool, error) {
	record, found, err := repository.BindReviewValidation(state.transaction).Exact(state.ctx,
		application.ReviewValidationRefreshLookup{
			ItemID: state.itemID, SourceSnapshotID: state.effectiveSnapshotID,
			TargetPlatformInstanceID: state.targetID, CoreID: state.coreID,
			ProviderID: state.providerID, TargetID: state.runtimeTargetID,
			DATVersionID: nullStringPointer(state.datID), DefaultDOSEntry: nullStringPointer(state.dosEntry),
		})
	if err != nil {
		return "", false, fmt.Errorf("libraryimport/review: %w", err)
	}
	if !found {
		return "", false, nil
	}
	state.sourceID = record.ID
	state.sourceManifestDigest = record.SourceManifestDigest
	state.sourceInputDigest = record.PrepublishInputDigest
	state.sourceStatus = record.Status
	state.compatibilityCode = record.CompatibilityCode
	state.dependencySnapshot = record.DependencySnapshot
	dependencyState, err := state.resolveDependencyState()
	if err != nil {
		return "", false, err
	}
	state.dependencyState = dependencyState
	if !state.exactValidationCurrent() {
		return "", false, nil
	}
	// Trial-required projects are current even though their status is BLOCKED.
	// Reusing that validation keeps the existing screenshot and approval decision.
	dependenciesUnchanged := !dependencyState.tracked ||
		(dependencyState.snapshotJSON == state.dependencySnapshot &&
			dependencyState.status == state.sourceStatus && dependencyState.code == state.compatibilityCode)
	if !dependenciesUnchanged {
		return "", false, nil
	}
	if state.sourceStatus == "READY" {
		return state.sourceID, true, nil
	}
	return "", true, nil
}

func (state *draftValidationRefresh) exactValidationCurrent() bool {
	return prepublishDigestMatches(state.sourceInputDigest, prepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: state.effectiveSnapshotID,
		SourceManifestDigest: state.sourceManifestDigest, ContentKind: state.contentKind,
		TargetPlatformInstanceID: state.targetID,
		ProviderID:               state.providerID, TargetID: state.runtimeTargetID,
		ContentPolicyDigest: state.contentPolicy.DigestFor(state.contentKind),
		DATVersionID:        nullStringPointer(state.datID), DefaultDOSEntry: nullStringPointer(state.dosEntry),
		DependencySnapshot: json.RawMessage(state.dependencySnapshot), Status: state.sourceStatus,
		CompatibilityCode: state.compatibilityCode,
	})
}

func (state *draftValidationRefresh) loadFallbackValidation() error {
	if state.sourceID != "" && (state.sourceStatus == "READY" || state.dependencyState.tracked) {
		return nil
	}
	record, found, err := repository.BindReviewValidation(state.transaction).Fallback(state.ctx,
		application.ReviewValidationRefreshLookup{
			ItemID: state.itemID, SourceSnapshotID: state.effectiveSnapshotID,
			CoreID: state.coreID, ProviderID: state.providerID, TargetID: state.runtimeTargetID,
			DATVersionID: nullStringPointer(state.datID),
		})
	if err != nil {
		return fmt.Errorf("libraryimport/review: %w", err)
	}
	if !found {
		state.sourceID = ""
		return nil
	}
	state.sourceID = record.ID
	state.sourceManifestDigest = record.SourceManifestDigest
	state.sourceInputDigest = record.PrepublishInputDigest
	state.sourceStatus = record.Status
	state.compatibilityCode = record.CompatibilityCode
	state.dependencySnapshot = record.DependencySnapshot
	return nil
}

func (state *draftValidationRefresh) resolveDependencyState() (draftDependencyState, error) {
	if state.contentKind == "SCUMMVM_PROJECT" {
		return state.resolveScummVMSelection()
	}
	if state.contentKind == "RPG_MAKER_PROJECT" {
		return state.resolveRPGDependencies()
	}
	return resolveDraftBIOSState(state.ctx, state.transaction, state.effectiveSnapshotID, state.providerID,
		state.runtimeTargetID, state.dependencySnapshot, state.sourceStatus, state.compatibilityCode)
}

func (state *draftValidationRefresh) resolveDependencies() error {
	dependencyState, err := state.resolveDependencyState()
	if err != nil {
		return err
	}
	state.dependencyState = dependencyState
	if dependencyState.tracked {
		state.sourceStatus = dependencyState.status
		state.compatibilityCode = dependencyState.code
		state.dependencySnapshot = dependencyState.snapshotJSON
	}
	return nil
}

func (state *draftValidationRefresh) insertValidation() (string, error) {
	createdID, _ := uuid.NewV7()
	now := state.service.now().UnixMilli()
	digest := prepublishDigest(state.digestInput())
	repository := repository.BindReviewValidation(state.transaction)
	err := repository.Create(state.ctx, application.ReviewValidationRefreshCreate{
		ID: createdID.String(), ItemID: state.itemID, TargetPlatformInstanceID: state.targetID,
		PlatformInstanceVersion: state.platformVersion, CoreID: state.coreID,
		ProviderID: state.providerID, TargetID: state.runtimeTargetID,
		DATVersionID: nullStringPointer(state.datID), DefaultDOSEntry: nullStringPointer(state.dosEntry),
		SourceManifestDigest: state.effectiveManifestDigest, SourceSnapshotID: state.effectiveSnapshotID,
		PrepublishInputDigest: digest, Status: state.sourceStatus,
		CompatibilityCode: state.compatibilityCode, DependencySnapshotJSON: state.dependencySnapshot,
		CreatedAtMS: now,
	})
	if err != nil {
		return "", fmt.Errorf("libraryimport/review: %w", err)
	}
	if err := repository.CopyFiles(state.ctx, application.ReviewValidationRefreshFileCopy{
		ValidationID: createdID.String(), SourceValidationID: state.sourceID, CreatedAtMS: now,
		ReplaceBIOSBundle: state.dependencyState.replaceBundle,
		Dependencies:      state.dependencyState.dependencies,
	}); err != nil {
		return "", fmt.Errorf("copy review validation files: %w", err)
	}
	if state.sourceStatus != "READY" {
		return "", nil
	}
	return createdID.String(), nil
}

func (state *draftValidationRefresh) digestInput() prepublishDigestInput {
	return prepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: state.effectiveSnapshotID,
		SourceManifestDigest: state.effectiveManifestDigest, ContentKind: state.contentKind,
		TargetPlatformInstanceID: state.targetID,
		ProviderID:               state.providerID, TargetID: state.runtimeTargetID,
		ContentPolicyDigest: state.contentPolicy.DigestFor(state.contentKind),
		DATVersionID:        nullStringPointer(state.datID), DefaultDOSEntry: nullStringPointer(state.dosEntry),
		DependencySnapshot: json.RawMessage(state.dependencySnapshot), Status: state.sourceStatus,
		CompatibilityCode: state.compatibilityCode,
	}
}

type draftDependencyState struct {
	tracked       bool
	replaceBundle bool
	snapshotJSON  string
	status        string
	code          string
	dependencies  []corevalidation.BIOSDependency
}

func resolveDraftBIOSState(
	ctx context.Context,
	transaction dbexec.Executor,
	sourceSnapshotID, providerID, targetID, previousSnapshot, previousStatus, previousCode string,
) (draftDependencyState, error) {
	if !isStaticBIOSSnapshot(previousSnapshot) {
		return resolveArcadeDraftBIOSState(
			ctx, transaction, providerID, targetID, previousSnapshot, previousStatus, previousCode,
		)
	}
	logicalName, err := snapshotContentLogicalName(ctx, transaction, sourceSnapshotID)
	if err != nil {
		return draftDependencyState{}, err
	}
	snapshot, status, code, err := validationservice.New(
		validationpersistence.New(
			transaction,
		),
	).ResolveBIOS(
		ctx,
		providerID,
		targetID,
		logicalName,
	)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("libraryimport/review: %w", err)
	}
	return draftDependencyState{
		tracked:       true,
		replaceBundle: true,
		snapshotJSON:  string(encoded),
		status:        status,
		code:          code,
		dependencies:  snapshot.BIOS,
	}, nil
}

// snapshotContentLogicalName returns the stable content identity used by
// conditional static dependency rules. DOS snapshots do not have a CONTENT
// row, so their first deterministic DOS_SOURCE is the bundle identity.
func snapshotContentLogicalName(
	ctx context.Context,
	transaction dbexec.Executor,
	sourceSnapshotID string,
) (string, error) {
	logicalName, err := repository.BindReviewValidation(transaction).ContentLogicalName(ctx, sourceSnapshotID)
	if err != nil {
		return "", fmt.Errorf("libraryimport/review: %w", err)
	}
	return logicalName, nil
}

type (
	arcadeDraftDependency = application.ArcadeDraftDependency
	arcadeDraftSnapshot   = application.ArcadeDraftSnapshot
)

func resolveArcadeDraftBIOSState(
	ctx context.Context,
	transaction dbexec.Executor,
	providerID, targetID, previousSnapshot, previousStatus, previousCode string,
) (draftDependencyState, error) {
	resolved, err := application.ResolveCreationArcade(ctx, repository.BindCreationArcade(transaction),
		providerID, targetID, previousSnapshot, previousStatus, previousCode)
	if err != nil {
		return draftDependencyState{}, fmt.Errorf("resolve arcade BIOS: %w", err)
	}
	return draftDependencyState{
		tracked: resolved.Tracked, status: resolved.Status, code: resolved.Code,
		snapshotJSON: resolved.SnapshotJSON, dependencies: resolved.Dependencies,
	}, nil
}

func parseArcadeDraftSnapshot(raw string) (arcadeDraftSnapshot, bool) {
	return application.ParseArcadeDraftSnapshot(raw)
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func isStaticBIOSSnapshot(raw string) bool {
	_, err := corevalidation.ParseSnapshot(raw)
	return err == nil
}

func nullableCandidate(value *string) sql.NullString {
	if value == nil || *value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
