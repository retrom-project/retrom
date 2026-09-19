package gamecontent

import (
	"context"
	"errors"
	"testing"

	contentvalidation "retrom/internal/capability/content/corevalidation"
	validation "retrom/internal/model/corevalidation"
	model "retrom/internal/model/gamecontent"
)

type replacementBIOSFacts struct {
	records          []validation.BIOSRecord
	failure          error
	calls            int
	provider, target string
}

func (facts *replacementBIOSFacts) BIOS(_ context.Context, provider, target string) ([]validation.BIOSRecord, error) {
	facts.calls++
	facts.provider, facts.target = provider, target
	return facts.records, facts.failure
}

func TestReplacementBIOSRejectsIncompleteIdentityBeforeFacts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ provider, target, content string }{
		{"", "target", "game.gba"}, {"provider", "", "game.gba"}, {"provider", "target", ""},
	} {
		facts := &replacementBIOSFacts{}
		_, err := replacementDependencies(t.Context(), model.WriteScope{ReadScope: model.ReadScope{BIOS: facts}},
			model.JobSnapshot{ProviderID: test.provider, TargetID: test.target},
			model.PreparedReplacement{FirstContentLogicalName: test.content}, model.Binding{})
		if !errors.Is(err, contentvalidation.ErrInvalidSnapshot) || facts.calls != 0 {
			t.Fatalf("input=%+v error=%v reads=%d", test, err, facts.calls)
		}
	}
}

func TestReplacementBIOSFactsPreserveReadCauseAndBlockedCode(t *testing.T) {
	t.Parallel()
	cause := errors.New("replacement BIOS unavailable")
	for _, test := range []struct {
		name    string
		records []validation.BIOSRecord
		failure error
	}{
		{"read failure", nil, cause},
		{"missing required", []validation.BIOSRecord{{Dependency: contentvalidation.BIOSDependency{
			BIOSCatalogEntry: contentvalidation.BIOSCatalogEntry{RequirementMode: "REQUIRED"},
		}}}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := &replacementBIOSFacts{records: test.records, failure: test.failure}
			_, err := replacementDependencies(t.Context(), model.WriteScope{ReadScope: model.ReadScope{BIOS: facts}},
				model.JobSnapshot{ProviderID: "provider", TargetID: "target"},
				model.PreparedReplacement{FirstContentLogicalName: "game.gba"}, model.Binding{})
			if facts.calls != 1 || facts.provider != "provider" || facts.target != "target" {
				t.Fatalf("facts=%+v", facts)
			}
			if test.failure != nil {
				if !errors.Is(err, cause) || err.Error() != "resolve replacement dependencies: corevalidation/read BIOS: "+cause.Error() {
					t.Fatalf("read error=%v", err)
				}
				return
			}
			var blocked *replacementValidationError
			if !errors.As(err, &blocked) || blocked.code != "LAUNCH_BIOS_MISSING" {
				t.Fatalf("blocked error=%v", err)
			}
		})
	}
}

func TestReplacementBIOSFiltersInapplicableOptionsBeforeDecoding(t *testing.T) {
	t.Parallel()
	condition, malformed := "PCE_CD_CONTENT", "invalid-json"
	facts := &replacementBIOSFacts{records: []validation.BIOSRecord{{Dependency: contentvalidation.BIOSDependency{
		BIOSCatalogEntry: contentvalidation.BIOSCatalogEntry{ConditionCode: &condition, RequirementMode: "REQUIRED"},
	}, ActivationOptions: &malformed}}}
	encoded, err := replacementDependencies(t.Context(), model.WriteScope{ReadScope: model.ReadScope{BIOS: facts}},
		model.JobSnapshot{ProviderID: "provider", TargetID: "target"},
		model.PreparedReplacement{FirstContentLogicalName: "cartridge.pce"}, model.Binding{})
	if err != nil || string(encoded) != "{\"schemaVersion\":1,\"kind\":\"STATIC\",\"bios\":[]}" || facts.calls != 1 {
		t.Fatalf("snapshot=%s error=%v reads=%d", encoded, err, facts.calls)
	}
}
