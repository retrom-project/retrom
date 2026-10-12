package runs

import (
	"testing"

	"retrom/internal/model"
)

func TestRestoreProjectionSeparatesWarningAndFailure(t *testing.T) {
	warning, reason := "CORE_CHANGED", "CONTENT_CHANGED"
	save := model.Save{Extinfo: model.Extinfo{CoreFingerprint: "original"}}
	RestoreResult{Restorable: true, Warning: &warning}.Apply(&save)
	if !save.Restorable || save.RestoreWarning != "core_changed" || save.RestoreReason != "" {
		t.Fatalf("warning: %+v", save)
	}
	RestoreResult{Reason: &reason, Warning: &warning}.Apply(&save)
	if save.Restorable || save.RestoreWarning != "" || save.RestoreReason != "content_changed" {
		t.Fatalf("blocked: %+v", save)
	}
	RestoreResult{Restorable: true}.Apply(&save)
	if !save.Restorable || save.RestoreWarning != "" || save.RestoreReason != "" || save.Extinfo.CoreFingerprint != "original" {
		t.Fatalf("cleared: %+v", save)
	}
}
