package firmware

import (
	"context"
	"errors"
	"testing"

	blobmodel "retrom/internal/model/blob"
	model "retrom/internal/model/firmware"

	"retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"
)

func TestServerReplacementPreservesEqualQualityAndMissingEvidence(t *testing.T) {
	for _, test := range []struct {
		name, digest, outcome, code string
		replace, evidence           bool
	}{
		{"preserve current", "new", "SKIPPED_EXISTING", "", false, false},
		{"same bytes", "old", "ALREADY_SAME_BYTES", "", true, false},
		{"missing evidence", "new", "SKIPPED_NOT_BETTER", "BIOS_CURRENT_EVIDENCE_INCOMPLETE", true, false},
		{"equal quality", "new", "SKIPPED_NOT_BETTER", "", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := model.ServerInstallRequest{
				SourceKind: "STATIC", ReplaceIfBetter: test.replace,
				Metadata: blobmodel.PreparedBlob{SHA256: test.digest}, Status: "MATCHED",
			}
			if test.evidence {
				expectation := firmware.StaticExpectation{LogicalName: "bios.bin"}
				candidate := firmware.EvaluateStatic(expectation, firmware.FileFacts{Basename: "bios.bin", SizeBytes: 10})
				request.StaticExpectation, request.StaticEvaluation = &expectation, &candidate
			}
			active := model.ActiveInstallation{
				ID: "active", SHA256: "old", Status: "MATCHED", ValidatedVersion: 1,
				Filename: "bios.bin", Size: 10,
			}
			result, handled, err := evaluateExistingInstallation(t.Context(), nil, request, 1, active, true)
			if err != nil || !handled || result.Outcome != test.outcome || result.OutcomeCode != test.code ||
				result.PreviousInstallationID != "active" || result.NewInstallationID != "" {
				t.Fatalf("replacement=%+v handled=%v error=%v", result, handled, err)
			}
		})
	}
}

func TestServerReplacementRefreshesChangedValidationForSameBytes(t *testing.T) {
	request := model.ServerInstallRequest{ReplaceIfBetter: true, Metadata: blobmodel.PreparedBlob{SHA256: "same"}, Status: "MATCHED"}
	active := model.ActiveInstallation{ID: "active", SHA256: "same", ValidatedVersion: 1, Status: "MATCHED"}
	result, handled, err := evaluateExistingInstallation(t.Context(), nil, request, 2, active, true)
	if err != nil || handled || result.PreviousInstallationID != "active" {
		t.Fatalf("stale validation retained: %+v handled=%v error=%v", result, handled, err)
	}
}

func TestReplacementArchiveReadPreservesFailure(t *testing.T) {
	failure := errors.New("archive records unavailable")
	request := model.ServerInstallRequest{
		SourceKind: "DAT_MACHINE", DATEvaluation: &firmware.DATEvaluation{},
		DATExpectedEntries: []firmware.ExpectedDATEntry{{Name: "bios.bin"}},
	}
	_, _, err := candidateStrictlyBetter(t.Context(), archiveFailure{failure}, request, "blob", firmware.FileFacts{})
	if !errors.Is(err, failure) {
		t.Fatalf("comparison swallowed storage failure: %v", err)
	}
}

type archiveFailure struct{ err error }

func (reader archiveFailure) Entries(context.Context, string) ([]importing.ArchiveEntry, error) {
	return nil, reader.err
}
