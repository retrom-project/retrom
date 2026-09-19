package firmwaremanifest

import (
	"strings"
	"testing"
)

func TestMemberDeclarationRejectsUnsafeAndUnverifiableValues(t *testing.T) {
	base := Member{Name: "boot.rom", SizeBytes: 16, CRC32: "12345678", SHA1: strings.Repeat("a", 40), Required: true}
	for _, name := range []string{".", "..", "../boot.rom", "/boot.rom", "a\\boot.rom", "a/../boot.rom", "bad\x00.rom"} {
		t.Run(name, func(t *testing.T) {
			value := base
			value.Name = name
			if err := ValidateMembers([]Member{value}); err == nil {
				t.Fatal("unsafe member accepted")
			}
		})
	}
	for _, members := range [][]Member{nil, {base, base}, {{Name: "boot.rom", SizeBytes: 16, CRC32: "12345678", SHA1: strings.Repeat("x", 40), Required: true}}, {{Name: "boot.rom", SizeBytes: 16, CRC32: "12345678", SHA1: strings.Repeat("a", 40)}}} {
		if err := ValidateMembers(members); err == nil {
			t.Fatalf("invalid members accepted: %#v", members)
		}
	}
}
