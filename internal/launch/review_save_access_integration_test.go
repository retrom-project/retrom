//go:build integration

package launch

import (
	"errors"
	"testing"
	"time"
)

func TestAuthorizeSaveRecognizesOrdinaryReviewSessions(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "save-access")
	err := fixture.launcher.AuthorizeSave(t.Context(), preview.PreviewID, preview.Capability)
	if err != nil {
		t.Fatalf("ordinary review save authorization: %v", err)
	}
	for _, capability := range []string{"", "incorrect"} {
		if err := fixture.launcher.AuthorizeSave(t.Context(), preview.PreviewID, capability); !errors.Is(err, ErrCredential) {
			t.Fatalf("invalid preview capability accepted: %v", err)
		}
	}
	*fixture.now = fixture.now.Add(3 * time.Hour)
	if err := fixture.launcher.AuthorizeSave(t.Context(), preview.PreviewID, preview.Capability); !errors.Is(err, ErrCredential) {
		t.Fatalf("expired preview accepted: %v", err)
	}
}

func TestAuthorizeSaveRejectsClosedReviewSessions(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "close-access")
	if _, err := fixture.launcher.RecordPlay(t.Context(), preview.PreviewID, preview.Capability, "finish",
		PlayEvent{ClientSequence: 0, ClientObservedAtMS: fixture.now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.launcher.AuthorizeSave(t.Context(), preview.PreviewID, preview.Capability); !errors.Is(err, ErrCredential) {
		t.Fatalf("closed preview accepted: %v", err)
	}
}
