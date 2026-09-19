package zipentry

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestDecodeNamePreservesLegacyContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		hex     string
		nonUTF8 bool
		want    string
		wantErr bool
	}{
		{name: "ascii", value: "game.gba", want: "game.gba"},
		{name: "ascii marked non-UTF-8", value: "game.gba", nonUTF8: true, want: "game.gba"},
		{name: "unicode", value: "雪/RPG制造.gba", want: "雪/RPG制造.gba"},
		{name: "unicode marked non-UTF-8", value: "雪/RPG制造.gba", nonUTF8: true, want: "雪/RPG制造.gba"},
		{name: "valid UTF-8 replacement rune", value: "fixture�.gba", nonUTF8: true, want: "fixture�.gba"},
		{name: "invalid UTF-8 without legacy flag", hex: "ff", wantErr: true},
		{name: "GB18030 legacy name", hex: "525047d6c6d4ec2e676261", nonUTF8: true, want: "RPG制造.gba"},
		{name: "GB18030 DOS path", hex: "bdf0d3b9c8bacfc0b4ab2f504c41592e424154", nonUTF8: true, want: "金庸群侠传/PLAY.BAT"},
		{name: "truncated GB18030 lead", hex: "81", nonUTF8: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := test.value
			if test.hex != "" {
				encoded, err := hex.DecodeString(test.hex)
				if err != nil {
					t.Fatal(err)
				}
				value = string(encoded)
			}
			got, err := DecodeName(value, test.nonUTF8)
			if test.wantErr {
				if got != "" || reflect.TypeOf(err) != reflect.TypeOf(ErrInvalidName) || !errors.Is(err, ErrInvalidName) {
					t.Fatalf("DecodeName() = %q, %v; want empty value, %v", got, err, ErrInvalidName)
				}
				if err.Error() != "ZIP_ENTRY_NAME_INVALID" {
					t.Fatalf("error text = %q", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("DecodeName() = %q, %v; want %q, nil", got, err, test.want)
			}
		})
	}
}
