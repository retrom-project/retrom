package corevalidation

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/capability/content/corevalidation"
	model "retrom/internal/model/corevalidation"
)

type biosMemory struct {
	records []model.BIOSRecord
	reads   int
}

func (memory *biosMemory) Catalog(context.Context, string, string) ([]corevalidation.BIOSCatalogEntry, error) {
	return nil, nil
}

func (memory *biosMemory) BIOS(context.Context, string, string) ([]model.BIOSRecord, error) {
	memory.reads++
	return memory.records, nil
}
func stringValue(value string) *string { return &value }

func TestBIOSRulesRunWithoutDatabase(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, mode   string
		status, blob *string
		want         string
	}{
		{name: "missing required", mode: "REQUIRED", want: "BLOCKED"},
		{name: "absent optional", mode: "OPTIONAL", want: "READY"},
		{name: "broken optional", mode: "OPTIONAL", status: stringValue("BROKEN"), blob: stringValue("blob"), want: "BLOCKED"},
		{name: "hash warning advisory", mode: "REQUIRED", status: stringValue("HASH_WARNING"), blob: stringValue("blob"), want: "READY"},
		{name: "missing entries advisory", mode: "REQUIRED", status: stringValue("MISSING_ENTRY"), blob: stringValue("blob"), want: "READY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := &biosMemory{records: []model.BIOSRecord{{Dependency: corevalidation.BIOSDependency{
				BIOSCatalogEntry:   corevalidation.BIOSCatalogEntry{RequirementID: "bios", RequirementMode: test.mode},
				InstallationStatus: test.status, BlobID: test.blob,
			}}}}
			snapshot, status, _, err := New(memory).ResolveBIOS(t.Context(), "provider", "target", "game.chd")
			if err != nil || status != test.want || len(snapshot.BIOS) != 1 {
				t.Fatalf("snapshot=%+v status=%s error=%v", snapshot, status, err)
			}
		})
	}
}

func TestInapplicableBIOSOptionsAreNotParsed(t *testing.T) {
	t.Parallel()
	memory := &biosMemory{records: []model.BIOSRecord{{Dependency: corevalidation.BIOSDependency{
		BIOSCatalogEntry: corevalidation.BIOSCatalogEntry{ConditionCode: stringValue("PCE_CD_CONTENT"), RequirementMode: "REQUIRED"},
	}, ActivationOptions: stringValue("invalid-json")}}}
	snapshot, status, _, err := New(memory).ResolveBIOS(t.Context(), "provider", "target", "cartridge.pce")
	if err != nil || status != "READY" || len(snapshot.BIOS) != 0 {
		t.Fatalf("snapshot=%+v status=%s error=%v", snapshot, status, err)
	}
	_, status, code, err := New(memory).ResolveBIOS(t.Context(), "provider", "target", "disc.chd")
	if !errors.Is(err, corevalidation.ErrInvalidSnapshot) || status != "BLOCKED" || code != "LAUNCH_CORE_VALIDATION_UNAVAILABLE" {
		t.Fatalf("applicable malformed options: status=%s code=%s error=%v", status, code, err)
	}
}

func TestIncompleteTargetDoesNotQueryBIOS(t *testing.T) {
	t.Parallel()
	memory := &biosMemory{}
	_, _, _, err := New(memory).ResolveBIOS(t.Context(), "", "target", "game.chd")
	if !errors.Is(err, corevalidation.ErrInvalidSnapshot) || memory.reads != 0 {
		t.Fatalf("error=%v reads=%d", err, memory.reads)
	}
}
