package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	contentvalidation "retrom/internal/capability/content/corevalidation"
	validation "retrom/internal/model/corevalidation"
	model "retrom/internal/model/libraryimport"
)

type importBIOSFacts struct {
	records []validation.BIOSRecord
	failure error
	queries [][2]string
	ctx     context.Context
}

func (facts *importBIOSFacts) BIOS(ctx context.Context, provider, target string) ([]validation.BIOSRecord, error) {
	facts.ctx = ctx
	facts.queries = append(facts.queries, [2]string{provider, target})
	return facts.records, facts.failure
}

type importBIOSCatalog struct {
	*importBIOSFacts
	catalog []contentvalidation.BIOSCatalogEntry
	failure error
	queries [][2]string
}

func (facts *importBIOSCatalog) Catalog(_ context.Context, provider, target string) ([]contentvalidation.BIOSCatalogEntry, error) {
	facts.queries = append(facts.queries, [2]string{provider, target})
	return facts.catalog, facts.failure
}

func TestCreationBIOSRejectsIncompleteIdentityBeforeFacts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, provider, target, content string
		want                            error
	}{
		{"provider", "", "target", "game.gba", contentvalidation.ErrInvalidSnapshot},
		{"target", "provider", "", "game.gba", contentvalidation.ErrInvalidSnapshot},
		{"content", "provider", "target", "", model.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := &importBIOSFacts{}
			groups := []model.PreparedGroup{{Sources: []model.PreparedSource{{Role: "CONTENT", LogicalName: test.content}}}}
			err := PrepareCreationStaticBIOS(t.Context(), facts,
				model.ImportTarget{PlatformID: "gba", ProviderID: test.provider, TargetID: test.target}, groups)
			if !errors.Is(err, test.want) || len(facts.queries) != 0 {
				t.Fatalf("error=%v queries=%v", err, facts.queries)
			}
		})
	}
}

func TestCreationBIOSUsesFactsAndPreservesReadFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("installation facts unavailable")
	plan := creationPreparedInput()
	facts := &importBIOSFacts{failure: cause}
	err := PrepareCreationStaticBIOS(t.Context(), facts, plan.Target, plan.Groups)
	if !errors.Is(err, cause) || err.Error() != "prepare creation static b i o s: corevalidation/read BIOS: "+cause.Error() {
		t.Fatalf("failure changed: %v", err)
	}
	if facts.ctx != t.Context() || !slices.Equal(facts.queries, [][2]string{{"provider", "target"}}) {
		t.Fatalf("read context=%v queries=%v", facts.ctx, facts.queries)
	}
}

func TestCreationBIOSKeepsOptionalAndConditionalRules(t *testing.T) {
	t.Parallel()
	condition, malformed := "PCE_CD_CONTENT", "invalid-json"
	for _, test := range []struct {
		name, mode, want   string
		condition, options *string
		biosCount          int
	}{
		{"absent optional", "OPTIONAL", "READY", nil, nil, 1},
		{"missing required", "REQUIRED", "BLOCKED", nil, nil, 1},
		{"inapplicable malformed", "REQUIRED", "READY", &condition, &malformed, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := creationPreparedInput()
			facts := &importBIOSFacts{records: []validation.BIOSRecord{{Dependency: contentvalidation.BIOSDependency{
				BIOSCatalogEntry: contentvalidation.BIOSCatalogEntry{RequirementMode: test.mode, ConditionCode: test.condition},
			}, ActivationOptions: test.options}}}
			if err := PrepareCreationStaticBIOS(t.Context(), facts, plan.Target, plan.Groups); err != nil {
				t.Fatal(err)
			}
			group := plan.Groups[0]
			snapshot, err := contentvalidation.ParseSnapshot(group.DependencySnapshot)
			if err != nil || group.ValidationStatus != test.want || len(snapshot.BIOS) != test.biosCount {
				t.Fatalf("group=%+v snapshot=%+v error=%v", group, snapshot, err)
			}
		})
	}
}

func TestCreationHeaderReadsCatalogWithoutResolvingBIOS(t *testing.T) {
	t.Parallel()
	cause := errors.New("catalog unavailable")
	for _, failure := range []error{nil, cause} {
		facts := &importBIOSCatalog{
			importBIOSFacts: &importBIOSFacts{},
			catalog:         []contentvalidation.BIOSCatalogEntry{{RequirementID: "current"}}, failure: failure,
		}
		run := creationCommit{plan: creationPreparedInput()}
		err := run.prepareHeader(t.Context(), model.ImportCreationScope{BIOS: facts})
		if !slices.Equal(facts.queries, [][2]string{{"provider", "target"}}) || len(facts.importBIOSFacts.queries) != 0 {
			t.Fatalf("catalog=%v BIOS=%v", facts.queries, facts.importBIOSFacts.queries)
		}
		if failure != nil {
			if !errors.Is(err, failure) || err.Error() != "prepare header: corevalidation/catalog: "+cause.Error() {
				t.Fatalf("catalog error=%v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			BIOSRequirements []contentvalidation.BIOSCatalogEntry
		}
		if err := json.Unmarshal([]byte(run.header.ConfigJSON), &document); err != nil || len(document.BIOSRequirements) != 1 || document.BIOSRequirements[0].RequirementID != "current" {
			t.Fatalf("config=%s error=%v", run.header.ConfigJSON, err)
		}
	}
}

type approvalContentName struct {
	model.ApprovalDependencyReader
	name    string
	failure error
	reads   int
}

func (reader *approvalContentName) LogicalName(context.Context, string) (string, error) {
	reader.reads++
	return reader.name, reader.failure
}

func TestApprovalBIOSPreservesReadOrderAndFailures(t *testing.T) {
	t.Parallel()
	cause := errors.New("snapshot read failed")
	emptySnapshot := "{\"schemaVersion\":1,\"kind\":\"STATIC\",\"bios\":[]}"
	for _, test := range []struct {
		name, provider, content, snapshot string
		nameFailure, biosFailure, want    error
		nameReads, biosReads              int
		errorText                         string
	}{
		{"current", "provider", "game.gba", emptySnapshot, nil, nil, nil, 1, 1, ""},
		{"bad snapshot", "provider", "game.gba", "broken", nil, nil, model.ErrInvalid, 0, 0, ""},
		{"name fails before identity", "", "game.gba", emptySnapshot, cause, nil, cause, 1, 0, "read approval content name: snapshot read failed"},
		{"missing provider", "", "game.gba", emptySnapshot, nil, nil, contentvalidation.ErrInvalidSnapshot, 1, 0, ""},
		{"missing content", "provider", "", emptySnapshot, nil, nil, contentvalidation.ErrInvalidSnapshot, 1, 0, ""},
		{"BIOS failure", "provider", "game.gba", emptySnapshot, nil, cause, cause, 1, 1, "resolve current approval BIOS: corevalidation/read BIOS: snapshot read failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &approvalContentName{name: test.content, failure: test.nameFailure}
			facts := &importBIOSFacts{failure: test.biosFailure}
			err := ValidateApprovalDependencies(t.Context(), model.ApprovalDependencyScope{Reader: reader, BIOS: facts},
				model.ApprovalDependencyInput{ProviderID: test.provider, TargetID: "target", ContentKind: "SINGLE_FILE", DependencyJSON: test.snapshot})
			if !errors.Is(err, test.want) || reader.reads != test.nameReads || len(facts.queries) != test.biosReads {
				t.Fatalf("error=%v name reads=%d BIOS reads=%v", err, reader.reads, facts.queries)
			}
			if test.errorText != "" && err.Error() != test.errorText {
				t.Fatalf("error text=%q", err.Error())
			}
		})
	}
}

func TestApprovalRejectsChangedBIOSFacts(t *testing.T) {
	t.Parallel()
	facts := &importBIOSFacts{records: []validation.BIOSRecord{{Dependency: contentvalidation.BIOSDependency{
		BIOSCatalogEntry: contentvalidation.BIOSCatalogEntry{RequirementMode: "REQUIRED"},
	}}}}
	err := ValidateApprovalDependencies(t.Context(),
		model.ApprovalDependencyScope{Reader: &approvalContentName{name: "game.gba"}, BIOS: facts},
		model.ApprovalDependencyInput{
			ProviderID: "provider", TargetID: "target", ContentKind: "SINGLE_FILE",
			DependencyJSON: "{\"schemaVersion\":1,\"kind\":\"STATIC\",\"bios\":[]}",
		})
	if !errors.Is(err, model.ErrInvalid) || len(facts.queries) != 1 {
		t.Fatalf("changed BIOS error=%v reads=%v", err, facts.queries)
	}
}
