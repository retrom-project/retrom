package corevalidation

import (
	"errors"
	"testing"

	"retrom/internal/capability/content/corevalidation"
)

func TestFrozenBIOSRecordsDoNotMutateTheirSource(t *testing.T) {
	options := `{"boot":"bios"}`
	records := []BIOSRecord{{Dependency: corevalidation.BIOSDependency{
		BIOSCatalogEntry:  corevalidation.BIOSCatalogEntry{RequirementID: "bios", RequirementMode: "OPTIONAL"},
		ActivationOptions: map[string]string{"original": "frozen"},
	}, ActivationOptions: &options}}
	first, status, _, err := ResolveBIOSRecords(records, "game.chd")
	if err != nil || status != "READY" || first.BIOS[0].ActivationOptions["boot"] != "bios" {
		t.Fatalf("first=%+v status=%s error=%v", first, status, err)
	}
	first.BIOS[0].ActivationOptions["boot"] = "changed"
	second, _, _, err := ResolveBIOSRecords(records, "game.chd")
	if err != nil || second.BIOS[0].ActivationOptions["boot"] != "bios" || records[0].Dependency.ActivationOptions["original"] != "frozen" {
		t.Fatalf("BIOS evaluation mutated frozen facts: second=%+v source=%+v error=%v", second, records, err)
	}
}

func TestFrozenBIOSRecordsRejectMissingLogicalName(t *testing.T) {
	_, status, code, err := ResolveBIOSRecords(nil, "")
	if !errors.Is(err, corevalidation.ErrInvalidSnapshot) || status != "BLOCKED" || code != "LAUNCH_CORE_VALIDATION_UNAVAILABLE" {
		t.Fatalf("status=%s code=%s error=%v", status, code, err)
	}
}
