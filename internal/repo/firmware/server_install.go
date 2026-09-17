package firmware

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/content/firmware"
	fwmodel "retrom/internal/model/firmware"

	"github.com/google/uuid"
)

func serverInstall(
	ctx context.Context,
	scope fwmodel.WriteScope,
	cmd fwmodel.ServerInstallCommand,
) (fwmodel.ServerInstallResult, error) {
	request := cmd.Request
	if err := scope.Server.LockExecution(ctx, fwmodel.ServerExecution{
		ImportID: request.ServerImportID, JobID: request.JobID, WorkerID: request.WorkerID,
		ExecutionNo: request.ExecutionNo, AtMS: cmd.NowFn(),
	}); err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("lock server BIOS execution: %w", err)
	}
	requirement, found, err := scope.Requirements.Get(ctx, request.RequirementID)
	if err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("recheck server BIOS catalog: %w", err)
	}
	if !found || !matchesServerCatalog(requirement, request) {
		return fwmodel.ServerInstallResult{}, fwmodel.ErrCatalogChanged
	}
	now := cmd.NowFn()
	if err := scope.Server.SelectCandidate(ctx, fwmodel.Selection{
		CandidateID: request.CandidateID, ImportID: request.ServerImportID,
		RequirementID: request.RequirementID, AtMS: now,
	}); err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("select server BIOS: %w", err)
	}
	active, exists, err := scope.ReadScope.Installations.Active(ctx, request.RequirementID)
	if err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("read active server BIOS: %w", err)
	}
	var handled bool
	result, handled, err := evaluateExistingInstallation(
		ctx,
		scope.ReadScope.Archives,
		request,
		requirement.Version,
		active,
		exists,
	)
	if err != nil {
		return fwmodel.ServerInstallResult{}, err
	}
	if !handled {
		result, err = persistServerInstallation(ctx, scope, request, requirement.Version, now, result)
		if err != nil {
			return fwmodel.ServerInstallResult{}, err
		}
	}
	if err := recordServerOutcome(ctx, scope.Server, request, result, now); err != nil {
		return fwmodel.ServerInstallResult{}, err
	}
	return result, nil
}

func matchesServerCatalog(requirement fwmodel.Requirement, request fwmodel.ServerInstallRequest) bool {
	return requirement.Enabled && requirement.SourceKind == request.SourceKind &&
		requirement.SourceVersion == request.SourceVersion && requirement.CatalogDigest == request.CatalogDigest &&
		requirement.Version == request.RequirementVersion && requirement.ProviderID == request.ProviderID &&
		requirement.TargetID == request.TargetID
}

func evaluateExistingInstallation(
	ctx context.Context,
	records fwmodel.ArchiveReader,
	request fwmodel.ServerInstallRequest,
	version int64,
	active fwmodel.ActiveInstallation,
	exists bool,
) (fwmodel.ServerInstallResult, bool, error) {
	if !exists {
		return fwmodel.ServerInstallResult{}, false, nil
	}
	result := fwmodel.ServerInstallResult{PreviousInstallationID: active.ID}
	if !request.ReplaceIfBetter {
		result.Outcome = "SKIPPED_EXISTING"
		return result, true, nil
	}
	if active.SHA256 == request.Metadata.SHA256 {
		if active.ValidatedVersion == version && active.Status == request.Status {
			result.Outcome = "ALREADY_SAME_BYTES"
			return result, true, nil
		}
		return result, false, nil
	}
	facts := firmware.FileFacts{
		Basename:  active.Filename,
		SizeBytes: active.Size,
		MD5:       active.MD5,
		SHA1:      active.SHA1,
		SHA256:    active.SHA256,
	}
	better, complete, err := candidateStrictlyBetter(ctx, records, request, active.BlobID, facts)
	if err != nil {
		return fwmodel.ServerInstallResult{}, false, err
	}
	if complete && better {
		return result, false, nil
	}
	result.Outcome = "SKIPPED_NOT_BETTER"
	if !complete {
		result.OutcomeCode = "BIOS_CURRENT_EVIDENCE_INCOMPLETE"
	}
	return result, true, nil
}

func candidateStrictlyBetter(ctx context.Context, records fwmodel.ArchiveReader, request fwmodel.ServerInstallRequest,
	activeBlobID string, facts firmware.FileFacts,
) (bool, bool, error) {
	if request.SourceKind == "STATIC" && request.ArchiveMembersJSON == nil {
		if request.StaticExpectation == nil || request.StaticEvaluation == nil {
			return false, false, nil
		}
		active := firmware.EvaluateStatic(*request.StaticExpectation, facts)
		return firmware.CompareStaticQuality(*request.StaticEvaluation, active) < 0, true, nil
	}
	if request.DATEvaluation == nil || len(request.DATExpectedEntries) == 0 {
		return false, false, nil
	}
	entries, err := records.Entries(ctx, activeBlobID)
	if err != nil {
		return false, false, fmt.Errorf("read current BIOS evidence: %w", err)
	}
	if len(entries) == 0 {
		return false, false, nil
	}
	active := firmware.EvaluateDAT(request.LogicalName, request.DATExpectedEntries, facts, entries)
	if request.ArchiveMembersJSON != nil {
		firmware.RequireCompleteArchive(&active)
	}
	return firmware.CompareDATQuality(*request.DATEvaluation, active) < 0, true, nil
}

func persistServerInstallation(ctx context.Context, scope fwmodel.WriteScope, request fwmodel.ServerInstallRequest,
	version, now int64, result fwmodel.ServerInstallResult,
) (fwmodel.ServerInstallResult, error) {
	blobID, err := scope.Blobs.Ensure(ctx, request.Metadata, now)
	if err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("register server BIOS blob: %w", err)
	}
	if request.SourceKind == "DAT_MACHINE" || request.ArchiveMembersJSON != nil {
		if err := scope.Archives.Put(ctx, blobID, request.ArchiveEntries, now); err != nil {
			return fwmodel.ServerInstallResult{}, fmt.Errorf("persist server BIOS archive: %w", err)
		}
	}
	if err := supersedeInScope(ctx, scope.Retirements, request.RequirementID, now); err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("retire server BIOS: %w", err)
	}
	request.Details["schemaVersion"] = 1
	request.Details["matchMethod"] = request.MatchMethod
	encoded, err := json.Marshal(request.Details)
	if err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("encode server BIOS evidence: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("generate server BIOS ID: %w", err)
	}
	if err := scope.Installations.Create(ctx, fwmodel.InstallationWrite{
		ID: id.String(), RequirementID: request.RequirementID, BlobID: blobID, Filename: request.OriginalFilename,
		MD5: request.Metadata.MD5, SHA1: request.Metadata.SHA1, SHA256: request.Metadata.SHA256, Size: request.Metadata.Size,
		RequirementVersion: version, Status: request.Status, DetailsJSON: encoded, AtMS: now, SourceKind: "SERVER_DIRECTORY",
		CandidateID: &request.CandidateID,
	}); err != nil {
		return fwmodel.ServerInstallResult{}, fmt.Errorf("persist server BIOS: %w", err)
	}
	result.NewInstallationID = id.String()
	switch request.Status {
	case "MATCHED":
		result.Outcome = "IMPORTED_MATCHED"
	case "HASH_WARNING":
		result.Outcome = "IMPORTED_WARNING"
	default:
		result.Outcome = "IMPORTED_MISSING_ENTRY"
	}
	return result, nil
}

func recordServerOutcome(ctx context.Context, records fwmodel.ServerRecords, request fwmodel.ServerInstallRequest,
	result fwmodel.ServerInstallResult, now int64,
) error {
	code := result.OutcomeCode
	if code == "" {
		switch result.Outcome {
		case "SKIPPED_EXISTING":
			code = "BIOS_EXISTING_INSTALLATION_PRESERVED"
		case "SKIPPED_NOT_BETTER":
			code = "BIOS_CANDIDATE_NOT_BETTER"
		case "ALREADY_SAME_BYTES":
			code = "BIOS_INSTALLATION_SAME_BYTES"
		default:
			code = result.Outcome
		}
	}
	details, err := json.Marshal(request.Details)
	if err != nil {
		return fmt.Errorf("encode BIOS result details: %w", err)
	}
	event, err := json.Marshal(map[string]any{"schemaVersion": 1, "phase": "INSTALLING", "result": result.Outcome})
	if err != nil {
		return fmt.Errorf("encode BIOS result event: %w", err)
	}
	if err := records.Finish(ctx, fwmodel.ServerOutcome{
		ImportID: request.ServerImportID, RequirementID: request.RequirementID,
		JobID: request.JobID, MatchMethod: request.MatchMethod, Result: result, Code: code,
		DetailsJSON: details, EventJSON: event, AtMS: now,
	}); err != nil {
		return fmt.Errorf("persist BIOS result and event: %w", err)
	}
	return nil
}
