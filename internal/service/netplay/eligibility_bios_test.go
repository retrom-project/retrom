package netplay

import (
	"errors"
	"testing"

	contentvalidation "retrom/internal/capability/content/corevalidation"
	validation "retrom/internal/model/corevalidation"
	model "retrom/internal/model/netplay"
)

func TestEligibilityBIOSRejectsIncompleteIdentityBeforeFacts(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"provider", "target", "content"} {
		t.Run(field, func(t *testing.T) {
			row := readyEligibilityRow()
			switch field {
			case "provider":
				row.ProviderID = ""
			case "target":
				row.TargetID = ""
			case "content":
				row.LogicalName = ""
			}
			facts := &eligibilityBIOS{}
			service := NewEligibility(nil, nil, nil, facts)
			current, err := service.dependencySnapshotCurrent(t.Context(), row)
			if current || !errors.Is(err, contentvalidation.ErrInvalidSnapshot) || facts.calls != 0 {
				t.Fatalf("current=%v error=%v reads=%d", current, err, facts.calls)
			}
		})
	}
}

func TestEligibilityBIOSChecksLockedSnapshotBeforeFacts(t *testing.T) {
	t.Parallel()
	row := readyEligibilityRow()
	row.ProviderID, row.DependencyJSON = "", "invalid"
	facts := &eligibilityBIOS{failure: errors.New("must not read")}
	service := NewEligibility(nil, nil, nil, facts)
	current, err := service.dependencySnapshotCurrent(t.Context(), row)
	if current || err != nil || facts.calls != 0 {
		t.Fatalf("current=%v error=%v reads=%d", current, err, facts.calls)
	}
}

func TestEligibilityBIOSPreservesReadCauseAndPrefix(t *testing.T) {
	t.Parallel()
	cause := errors.New("installation unavailable")
	facts := &eligibilityBIOS{failure: cause}
	service := NewEligibility(nil, nil, nil, facts)
	current, err := service.dependencySnapshotCurrent(t.Context(), readyEligibilityRow())
	if current || !errors.Is(err, cause) || err.Error() != "netplay/resolve BIOS snapshot: corevalidation/read BIOS: "+cause.Error() {
		t.Fatalf("current=%v error=%v", current, err)
	}
}

func TestEligibilityBIOSUsesTheConditionalModelRule(t *testing.T) {
	t.Parallel()
	condition, malformed := "PCE_CD_CONTENT", "invalid-json"
	for _, content := range []string{"cartridge.pce", "disc.chd"} {
		t.Run(content, func(t *testing.T) {
			row := readyEligibilityRow()
			row.LogicalName = content
			facts := &eligibilityBIOS{records: []validation.BIOSRecord{{Dependency: contentvalidation.BIOSDependency{
				BIOSCatalogEntry: contentvalidation.BIOSCatalogEntry{ConditionCode: &condition, RequirementMode: "REQUIRED"},
			}, ActivationOptions: &malformed}}}
			service := NewEligibility(nil, nil, nil, facts)
			current, err := service.dependencySnapshotCurrent(t.Context(), row)
			if content == "cartridge.pce" {
				if !current || err != nil {
					t.Fatalf("inapplicable current=%v error=%v", current, err)
				}
			} else if current || !errors.Is(err, contentvalidation.ErrInvalidSnapshot) {
				t.Fatalf("applicable current=%v error=%v", current, err)
			}
			if facts.calls != 1 {
				t.Fatalf("reads=%d", facts.calls)
			}
		})
	}
}

func TestRoomAndSessionBIOSUseTheirScopeFacts(t *testing.T) {
	t.Parallel()
	cause := errors.New("transaction BIOS facts unavailable")
	registry := eligibilityRegistry()
	for _, operation := range []string{"room", "session"} {
		t.Run(operation, func(t *testing.T) {
			repository := &eligibilityMemory{rows: map[string][]model.EligibilityRow{"game": {readyEligibilityRow()}}}
			facts := &eligibilityBIOS{failure: cause}
			var err error
			if operation == "room" {
				service := &RoomControl{registry: registry}
				_, err = service.eligible(t.Context(), model.RoomControlScope{Eligibility: repository, BIOS: facts}, "game")
			} else {
				service := &SessionStart{registry: registry}
				_, err = service.lockedProfile(t.Context(), model.SessionStartScope{Eligibility: repository, BIOS: facts},
					&model.RoomSelection{GameID: "game"})
			}
			if !errors.Is(err, cause) || facts.calls != 1 {
				t.Fatalf("error=%v reads=%d", err, facts.calls)
			}
		})
	}
}
