package contentprofile

import "testing"

func TestSymbianPreservesNativeInstaller(t *testing.T) {
	p, ok := ByPlatform("symbian")
	if !ok || p.ArchivePolicy != ArchiveNone || len(p.ArchiveFormats) != 0 {
		t.Fatalf("native package profile: %+v %v", p, ok)
	}
	if len(p.Extensions) != 2 || p.Extensions[0] != ".sis" || p.Extensions[1] != ".sisx" {
		t.Fatalf("extensions: %v", p.Extensions)
	}
}
