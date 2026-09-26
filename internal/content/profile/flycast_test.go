package contentprofile

import "testing"

func TestDreamcastAcceptsSingleCHD(t *testing.T) {
	if !AllowsContentKind("dreamcast", ContentKindSingleFile) {
		t.Fatal("Dreamcast must accept a single CHD")
	}
	if AllowsContentKind("dreamcast", ContentKindMultiDisc) {
		t.Fatal("unvalidated multi-disc support must not be advertised")
	}
	got := SupportedExtensions("dreamcast")
	if len(got) != 1 || got[0] != ".chd" {
		t.Fatalf("Dreamcast extensions = %v", got)
	}
}

func TestFlycastArcadePlatformsAcceptOpaqueCartridgeZIP(t *testing.T) {
	for _, platform := range []string{"naomi", "naomi2", "atomiswave"} {
		if !AllowsContentKind(platform, ContentKindSingleFile) ||
			AllowsContentKind(platform, ContentKindMultiDisc) ||
			!AcceptsRaw(platform, "machine.zip") || AcceptsRaw(platform, "disc.chd") {
			t.Fatalf("%s cartridge content contract is invalid", platform)
		}
		got := SupportedExtensions(platform)
		if len(got) != 1 || got[0] != ".zip" {
			t.Fatalf("%s extensions = %v", platform, got)
		}
	}
}
