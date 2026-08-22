package importing

import (
	"testing"

	"retrom/internal/testassert"
)

func TestValidateLogicalPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		valid bool
	}{
		{value: "GAME/DOOM2.WAD", valid: true},
		{value: "目录/游戏.gba", valid: true},
		{value: "literal/%2e%2e/game.rom", valid: true},
		{value: "../escape", valid: false},
		{value: "/absolute", valid: false},
		{value: "C:/drive", valid: false},
		{value: `dir\file`, valid: false},
		{value: "double//slash", valid: false},
		{value: "trailing/", valid: false},
		{value: "control\x01file", valid: false},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateLogicalPath(test.value)
			testassert.Falsef(t, (err == nil) != test.valid, "ValidateLogicalPath(%q) error = %v", test.value, err)
		})
	}
}
