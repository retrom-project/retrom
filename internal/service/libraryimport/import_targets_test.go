package libraryimport

import (
	"errors"
	"fmt"
	model "retrom/internal/model/libraryimport"
	"testing"
)

func TestImportAdmissionSnapshotsAllVirtualRPGTargets(t *testing.T) {
	t.Parallel()
	_, memory, _ := admissionServiceFixture()
	target := model.ImportTarget{ID: "rpg", PlatformID: "rpgmaker", DefaultCoreID: "rpgmaker", Version: 7}
	memory.bindings = nil
	for index := 6; index >= 0; index-- {
		memory.bindings = append(memory.bindings, model.ImportBinding{CoreID: "rpgmaker", ProviderID: "provider", TargetID: fmt.Sprint(index)})
	}
	snapshot, provisional, err := SnapshotImportTarget(t.Context(), memory, target)
	if err != nil || len(snapshot.Targets) != 7 || snapshot.PlatformInstanceVersion != 7 || provisional.TargetID != "0" || provisional.DefaultCoreID != "rpgmaker" {
		t.Fatalf("snapshot=%+v target=%+v err=%v", snapshot, provisional, err)
	}
	for index, guard := range snapshot.Targets {
		if guard.TargetID != fmt.Sprint(index) {
			t.Fatalf("target ordering=%v", snapshot.Targets)
		}
	}
	memory.bindings = memory.bindings[:6]
	if _, _, err := SnapshotImportTarget(t.Context(), memory, target); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("partial virtual targets err=%v", err)
	}
	cause := errors.New("bindings unavailable")
	memory.readError = cause
	if _, _, err := SnapshotImportTarget(t.Context(), memory, target); !errors.Is(err, cause) {
		t.Fatalf("bindings err=%v", err)
	}
}
