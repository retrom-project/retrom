package launch

import (
	"strings"
	"testing"
)

func TestROMByteIdentitySurvivesTargetAndProviderRevision(t *testing.T) {
	t.Parallel()
	base := ContentView{
		Digest: strings.Repeat("a", 64), Format: "RETROM_SINGLE_FILE_V1", CoreID: "fceumm",
		ProviderID: "emulatorjs", TargetID: "fceumm", BundleSHA256: strings.Repeat("b", 64),
	}
	want, err := ContentIdentity(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []ContentView{
		{Digest: base.Digest, Format: base.Format, CoreID: "nestopia", ProviderID: "emulatorjs", TargetID: "nestopia", BundleSHA256: base.BundleSHA256},
		{Digest: base.Digest, Format: base.Format, CoreID: base.CoreID, ProviderID: base.ProviderID, TargetID: base.TargetID, BundleSHA256: strings.Repeat("c", 64)},
	} {
		got, err := ContentIdentity(changed)
		if err != nil || got != want {
			t.Fatalf("same immutable bytes changed identity: %s != %s (%v)", got, want, err)
		}
	}
}
