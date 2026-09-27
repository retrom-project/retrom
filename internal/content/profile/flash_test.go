package contentprofile

import "testing"

func TestFlashAcceptsOnlySingleSWF(t *testing.T) {
	if !AcceptsRaw("flash", "Mode.SWF") || AcceptsRaw("flash", "game.swf.zip") || AcceptsRaw("flash", "game.exe") {
		t.Fatal("Flash must accept only a single SWF payload")
	}
	if AcceptsArchive("flash", ArchiveZIP) || !AllowsContentKind("flash", ContentKindSingleFile) {
		t.Fatal("Flash content policy mismatch")
	}
}
