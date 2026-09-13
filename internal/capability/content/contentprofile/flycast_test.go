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
