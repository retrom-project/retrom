package firmware

import (
	"context"
	"fmt"

	model "retrom/internal/model/firmware"

	"retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"
)

type archiveEntryReader interface {
	Entries(context.Context, string) ([]importing.ArchiveEntry, error)
}

func evaluateExistingInstallation(ctx context.Context, records archiveEntryReader, request model.ServerInstallRequest,
	version int64, active model.ActiveInstallation, exists bool,
) (model.ServerInstallResult, bool, error) {
	if !exists {
		return model.ServerInstallResult{}, false, nil
	}
	result := model.ServerInstallResult{PreviousInstallationID: active.ID}
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
		return model.ServerInstallResult{}, false, err
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

func candidateStrictlyBetter(ctx context.Context, records archiveEntryReader, request model.ServerInstallRequest,
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
