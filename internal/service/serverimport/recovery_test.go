package serverimport

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/serverimport"

	"retrom/internal/capability/content/firmware"
)

type recoveryMemory struct {
	phase   string
	records []model.CandidateEvidence
	entries []firmware.ExpectedDATEntry
	reads   int
	err     error
}

func (memory *recoveryMemory) Items(context.Context, string) ([]model.CatalogItem, error) {
	return nil, memory.err
}

func (memory *recoveryMemory) Phase(context.Context, string) (string, error) {
	return memory.phase, memory.err
}

func (memory *recoveryMemory) Candidates(context.Context, string) ([]model.CandidateEvidence, error) {
	return memory.records, memory.err
}

func (memory *recoveryMemory) DATEntries(context.Context, string, string) ([]firmware.ExpectedDATEntry, error) {
	memory.reads++
	return memory.entries, memory.err
}
func (*recoveryMemory) Path(sha string) string { return "cas/" + sha }

func TestRecoveryDistinguishesCompletedDiscoveryFromPartialWork(t *testing.T) {
	for _, phase := range []string{"", "PREPARING_ROOT", "DISCOVERING", "DISCOVERY_COMPLETED", "RANKING", "INSTALLING", "QUEUEING_REVALIDATION"} {
		memory := &recoveryMemory{phase: phase}
		result, err := NewRecovery(memory, memory).DiscoveryWasPersisted(t.Context(), "import")
		want := phase == "DISCOVERY_COMPLETED" || phase == "RANKING" || phase == "INSTALLING" || phase == "QUEUEING_REVALIDATION"
		if err != nil || result != want {
			t.Fatalf("phase %s: %v %v", phase, result, err)
		}
	}
}

func TestRecoveryRestoresStatusAndReadsDATOncePerRequirement(t *testing.T) {
	version, machine := "dat", "machine"
	item := model.CatalogItem{RequirementID: "requirement", SourceKind: "DAT_MACHINE", DATVersionID: &version, DATMachineName: &machine}
	evidence := model.CandidateEvidence{ID: "one", RequirementID: item.RequirementID, State: "ELIGIBLE", Facts: firmware.FileFacts{SHA256: "digest"}, DAT: &firmware.DATEvaluation{SafeArchive: true, Launchable: true, MismatchedCount: 1}}
	memory := &recoveryMemory{records: []model.CandidateEvidence{evidence, evidence}, entries: []firmware.ExpectedDATEntry{{Name: "boot.rom"}}}
	groups, err := NewRecovery(memory, memory).Candidates(t.Context(), "import", []model.CatalogItem{item})
	if err != nil {
		t.Fatal(err)
	}
	values := groups[item.RequirementID]
	if len(values) != 2 || memory.reads != 1 || values[0].DAT.Status != "HASH_WARNING" || values[0].DAT.Method != "DAT_ENTRY_WARNING" || values[0].Metadata.Path != "cas/digest" || values[0].ExpectedDATEntries[0].Name != "boot.rom" {
		t.Fatalf("restored evidence: %+v reads=%d", values, memory.reads)
	}
	if memory.records[0].DAT.Status != "" {
		t.Fatal("recovery mutated repository evidence")
	}
}

func TestRecoveryRejectsUnknownRequirementAndMissingEvaluation(t *testing.T) {
	memory := &recoveryMemory{records: []model.CandidateEvidence{{ID: "candidate", RequirementID: "missing", State: "ELIGIBLE"}}}
	service := NewRecovery(memory, memory)
	if _, err := service.Candidates(t.Context(), "import", nil); !errors.Is(err, model.ErrCatalogInvalid) {
		t.Fatalf("unknown requirement: %v", err)
	}
	if _, err := service.Candidates(t.Context(), "import", []model.CatalogItem{{RequirementID: "missing", SourceKind: "STATIC"}}); !errors.Is(err, model.ErrCatalogInvalid) {
		t.Fatalf("missing evaluation: %v", err)
	}
	memory.err = context.Canceled
	if _, err := service.Candidates(t.Context(), "import", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("read failure: %v", err)
	}
}

func TestRecoveryUsesFrozenStaticArchiveRequirements(t *testing.T) {
	members := `[{"name":"boot.rom","sizeBytes":1,"crc32":"12345678","sha1":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","required":true}]`
	memory := &recoveryMemory{}
	service := NewRecovery(memory, memory)
	entries, err := service.ExpectedDATEntries(t.Context(), model.CatalogItem{ArchiveMembersJSON: &members})
	if err != nil || len(entries) != 1 || entries[0].Name != "boot.rom" || memory.reads != 0 {
		t.Fatalf("static archive: %+v %v", entries, err)
	}
	version, machine := "dat", "machine"
	if _, err := service.ExpectedDATEntries(t.Context(), model.CatalogItem{DATVersionID: &version, DATMachineName: &machine}); !errors.Is(err, model.ErrCatalogInvalid) {
		t.Fatalf("empty DAT: %v", err)
	}
}
