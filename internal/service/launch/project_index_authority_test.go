package launch

import (
	"errors"
	model "retrom/internal/model/launch"
	"testing"
	"time"
)

func TestProjectIndexesPreservesAuthorityAndPreviewIsolation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*model.ProjectIndexSnapshot)
		ref    model.ProjectIndexReference
	}{
		{name: "bootstrap", change: func(s *model.ProjectIndexSnapshot) { s.Source.State = "CREATED" }},
		{name: "finished", change: func(s *model.ProjectIndexSnapshot) { s.Source.State = "FINISHED" }},
		{name: "hard expiry", change: func(s *model.ProjectIndexSnapshot) { s.Source.HardEnd = 1000 }},
		{name: "idle expiry", change: func(s *model.ProjectIndexSnapshot) { end := int64(1000); s.Source.IdleEnd = &end }},
		{name: "unknown purpose", change: func(s *model.ProjectIndexSnapshot) { s.Source.Purpose = "unknown" }},
		{name: "non project delivery", change: func(s *model.ProjectIndexSnapshot) { s.Source.Delivery = "ROM_BLOB" }},
		{name: "preview requires preview", ref: model.ProjectIndexReference{PreviewOnly: true}, change: func(*model.ProjectIndexSnapshot) {}},
		{name: "preview ONS only", ref: model.ProjectIndexReference{PreviewOnly: true}, change: func(s *model.ProjectIndexSnapshot) { s.Source.Purpose = "REVIEW_PREVIEW" }},
		{name: "preview mismatched format", change: func(s *model.ProjectIndexSnapshot) {
			s.Source.Purpose = "REVIEW_PREVIEW"
			s.Source.ContentKind = "ONS_PROJECT"
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			memory := indexMemoryFixture()
			item.change(&memory.snapshot)
			result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), item.ref, "valid")
			if !errors.Is(err, model.ErrCredential) || len(result.Contents) != 0 || result.SHA256 != "" {
				t.Fatalf("unauthorized result: %v", err)
			}
		})
	}
}

func TestProjectIndexesPreviewUsesHardExpiryWithoutProductIdleBudget(t *testing.T) {
	t.Parallel()
	memory := indexMemoryFixture()
	memory.snapshot.Source.Purpose = "REVIEW_PREVIEW"
	expired := int64(500)
	memory.snapshot.Source.IdleEnd = &expired
	result, err := projectIndexService(memory, func() time.Time { return time.UnixMilli(1000) }).Index(t.Context(), model.ProjectIndexReference{}, "valid")
	if err != nil || len(result.Contents) == 0 {
		t.Fatalf("preview inherited product idle budget: %v", err)
	}
}
