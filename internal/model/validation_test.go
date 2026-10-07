package model

import (
	"errors"
	"strings"
	"testing"
)

func TestSharedGameValidationMatchesPlayerAuthority(t *testing.T) {
	t.Parallel()
	players := strings.Repeat("玩", 64)
	input := GameInput{PlatformInstanceID: "11111111-1111-4111-8111-111111111111", Title: "Game", Players: &players}
	if err := ValidateGameFields(input); err != nil {
		t.Fatalf("64 character player value rejected: %v", err)
	}
	players += "玩"
	if err := ValidateGameFields(input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("65 character player value accepted: %v", err)
	}
	input.Players = nil
	input.Title = "bad\xfftitle"
	if err := ValidateGameFields(input); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid UTF8 title accepted: %v", err)
	}
}
