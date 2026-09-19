package corevalidation

import (
	"errors"
	"testing"

	contentvalidation "retrom/internal/capability/content/corevalidation"
)

func TestValidateBIOSRequestKeepsCompleteIdentityRequirement(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, provider, target, content string
		valid                           bool
	}{
		{"complete", "provider", "target", "game.rom", true},
		{"missing provider", "", "target", "game.rom", false},
		{"missing target", "provider", "", "game.rom", false},
		{"missing content", "provider", "target", "", false},
		{"empty", "", "", "", false},
		{"identity is not normalized", " ", " ", " ", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateBIOSRequest(test.provider, test.target, test.content)
			if test.valid && err != nil || !test.valid && !errors.Is(err, contentvalidation.ErrInvalidSnapshot) {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
