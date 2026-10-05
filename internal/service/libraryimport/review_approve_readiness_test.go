package libraryimport

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/content/requirements"
	corevalidation "retrom/internal/core/validation"
	"retrom/internal/format/nintendo3ds"
	biosservice "retrom/internal/service/corevalidation"
)

type readinessContent struct{ ApprovalDependencyReader }

func (readinessContent) LogicalName(context.Context, string) (string, error) { return "game.gba", nil }

type readinessBIOS struct{ biosservice.Repository }

func (readinessBIOS) BIOS(context.Context, string, string) ([]biosservice.BIOSRecord, error) {
	return []biosservice.BIOSRecord{}, nil
}

func TestReviewApprovalPreservesInspectedContentFacts(t *testing.T) {
	snapshot := corevalidation.Snapshot{
		SchemaVersion: 1, Kind: "STATIC", BIOS: []corevalidation.BIOSDependency{},
		ContentFacts: &requirements.Facts{Nintendo3DS: &nintendo3ds.Facts{Format: "NCSD"}},
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateApprovalDependencies(t.Context(), ApprovalDependencyScope{
		Reader: readinessContent{}, BIOS: readinessBIOS{},
	}, ApprovalDependencyInput{
		ProviderID: "emulatorjs", TargetID: "azahar",
		ContentKind: "SINGLE_FILE", DependencyJSON: string(encoded),
	})
	if err != nil {
		t.Fatalf("unchanged content inspection prevents approval: %v", err)
	}
}

func TestReviewApprovalRequiresReadyContentEvenWhenBIOSIsReady(t *testing.T) {
	encoded, err := (corevalidation.Snapshot{SchemaVersion: 1, Kind: "STATIC", BIOS: []corevalidation.BIOSDependency{}}).JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"BLOCKED", "INCOMPATIBLE", "READY"} {
		t.Run(status, func(t *testing.T) {
			run := reviewApprovalRun{ctx: t.Context(), head: ReviewApprovalHead{
				ValidationStatus: status, ContentKind: "SINGLE_FILE", PlatformID: "gba",
				ProviderID: "emulatorjs", TargetID: "mgba", DependencyJSON: string(encoded),
			}, scope: ReviewApprovalScope{Dependencies: ApprovalDependencyScope{Reader: readinessContent{}, BIOS: readinessBIOS{}}}}
			err := run.prepareValidation()
			if status == "READY" && err != nil || status != "READY" && !errors.Is(err, ErrInvalid) {
				t.Fatalf("content status=%s approval error=%v", status, err)
			}
		})
	}
}
