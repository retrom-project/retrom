package library

import (
	"encoding/json"
	"errors"
	"testing"

	"retrom/internal/model"
)

func TestLaunchOptionsChecksAdminBeforeReadingGameOrDraft(t *testing.T) {
	t.Parallel()
	s := &Service{}
	_, err := s.LaunchOptions(t.Context(), model.Principal{User: model.User{Role: "user"}}, "game", json.RawMessage(`{}`))
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("non-admin launch inspection = %v", err)
	}
}
