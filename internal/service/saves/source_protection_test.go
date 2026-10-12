package saves

import (
	"errors"
	"testing"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/service/runs"
)

func TestCrossVersionRestoreProtectsSource(t *testing.T) {
	run := runs.Context{Run: model.Run{Purpose: "play", Save: &model.Save{ID: "source", RestoreWarning: "core_changed"}}, Checkpoint: &runtimeclient.Checkpoint{Semantics: "GAME_SAVE"}}
	input := model.SaveInput{Name: "New progress", Kind: "game_save"}
	if err := validateInput(input, run, "source"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("source overwrite: %v", err)
	}
	if err := validateInput(input, run, "new-save"); err != nil {
		t.Fatalf("fork: %v", err)
	}
	run.Run.Save.RestoreWarning = ""
	if err := validateInput(input, run, "source"); err != nil {
		t.Fatalf("ordinary overwrite: %v", err)
	}
}
