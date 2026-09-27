package launch

import (
	"errors"
	"testing"
	"time"
)

func TestProjectIndexesPreservesAuthorityAndPreviewIsolation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*ProjectIndexSnapshot)
		ref    ProjectIndexReference
	}{
		{name: "bootstrap", change: func(s *ProjectIndexSnapshot) { s.Source.State = "CREATED" }},
		{name: "finished", change: func(s *ProjectIndexSnapshot) { s.Source.State = "FINISHED" }},
		{name: "hard expiry", change: func(s *ProjectIndexSnapshot) { s.Source.HardEnd = 1000 }},
		{name: "unknown purpose", change: func(s *ProjectIndexSnapshot) { s.Source.Purpose = "unknown" }},
		{name: "non project delivery", change: func(s *ProjectIndexSnapshot) { s.Source.Delivery = "ROM_BLOB" }},
		{name: "preview requires preview", ref: ProjectIndexReference{PreviewOnly: true}, change: func(*ProjectIndexSnapshot) {}},
		{name: "preview ONS only", ref: ProjectIndexReference{PreviewOnly: true}, change: func(s *ProjectIndexSnapshot) { s.Source.Purpose = "REVIEW_PREVIEW" }},
		{name: "preview mismatched format", change: func(s *ProjectIndexSnapshot) {
			s.Source.Purpose = "REVIEW_PREVIEW"
			s.Source.ContentKind = "ONS_PROJECT"
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			memory := indexMemoryFixture()
			item.change(&memory.snapshot)
			result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), item.ref, "valid")
			if !errors.Is(err, ErrCredential) || len(result.Contents) != 0 || result.SHA256 != "" {
				t.Fatalf("unauthorized result: %v", err)
			}
		})
	}
}

func TestProjectIndexesUseHardExpiryForActiveSessions(t *testing.T) {
	t.Parallel()
	for _, purpose := range []string{"PRODUCT", "REVIEW_PREVIEW"} {
		t.Run(purpose, func(t *testing.T) {
			memory := indexMemoryFixture()
			memory.snapshot.Source.Purpose = purpose
			memory.snapshot.Source.BootstrapEnd = 500
			for _, now := range []int64{1000, 1999, 2000} {
				result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(now) }).Index(t.Context(), ProjectIndexReference{}, "valid")
				if now < memory.snapshot.Source.HardEnd {
					if err != nil || len(result.Contents) == 0 {
						t.Fatalf("active content unavailable at %d: %v", now, err)
					}
				} else if !errors.Is(err, ErrCredential) || len(result.Contents) != 0 || result.SHA256 != "" {
					t.Fatalf("expired content authorized at %d: %v", now, err)
				}
			}
		})
	}
}
