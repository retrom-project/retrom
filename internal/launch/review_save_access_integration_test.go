//go:build integration

package launch

import (
	"errors"
	"testing"
	"time"
)

func TestSaveAccessRecognizesOrdinaryReviewSessions(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "save-access")
	access, err := fixture.launcher.SaveAccess(t.Context(), preview.PreviewID, preview.Capability)
	if err != nil || access != "NORMAL" {
		t.Fatalf("ordinary review save access = %q, %v", access, err)
	}
	for _, capability := range []string{"", "incorrect"} {
		if _, err := fixture.launcher.SaveAccess(t.Context(), preview.PreviewID, capability); !errors.Is(err, ErrCredential) {
			t.Fatalf("invalid preview capability accepted: %v", err)
		}
	}
	*fixture.now = fixture.now.Add(3 * time.Hour)
	if _, err := fixture.launcher.SaveAccess(t.Context(), preview.PreviewID, preview.Capability); !errors.Is(err, ErrCredential) {
		t.Fatalf("expired preview accepted: %v", err)
	}
}

func TestSaveAccessRejectsClosedReviewSessions(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "close-access")
	if _, err := fixture.launcher.RecordPlay(t.Context(), preview.PreviewID, preview.Capability, "finish",
		PlayEvent{ClientSequence: 0, ClientObservedAtMS: fixture.now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.launcher.SaveAccess(t.Context(), preview.PreviewID, preview.Capability); !errors.Is(err, ErrCredential) {
		t.Fatalf("closed preview accepted: %v", err)
	}
}
