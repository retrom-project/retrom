package arcadedat

import (
	"reflect"
	"testing"

	"retrom/internal/testassert"
)

func TestCoreFamiliesAreClosedAndComplete(t *testing.T) {
	t.Parallel()
	want := []string{"fbalpha2012_cps1", "fbalpha2012_cps2", "fbneo", "mame2003", "mame2003_plus"}
	if actual := CoreIDs(); !reflect.DeepEqual(actual, want) {
		t.Fatalf("CoreIDs() = %#v, want %#v", actual, want)
	}
	for _, coreID := range want {
		family, exists := FamilyForCore(coreID)
		testassert.Falsef(t, testassert.Any(func() bool { return !exists }, func() bool { return family == "" }, func() bool { return !SupportsCore(coreID) }), "supported core %q has family %q, exists=%v", coreID, family, exists)
	}
	for _, coreID := range []string{"azahar", "genesis_plus_gx_wide", "mame2010", ""} {
		if _, exists := FamilyForCore(coreID); exists || SupportsCore(coreID) {
			t.Fatalf("unsupported core %q was accepted", coreID)
		}
	}
}
