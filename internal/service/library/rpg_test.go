package library

import (
	"testing"

	"retrom/internal/model"
)

func TestRPGCandidatePermissionPrecedesLibraryReads(t *testing.T) {
	t.Parallel()
	service := &Service{}
	if _, err := service.RPGEntryCandidates(t.Context(), model.Principal{}, "game", "RPGMV"); err == nil {
		t.Fatal("anonymous inspection accepted")
	}
}
