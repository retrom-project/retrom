package library

import (
	"errors"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestKirikiriEntryPermissionAndBoundsPrecedeContentReads(t *testing.T) {
	t.Parallel()
	s := &Service{}
	_, err := s.KirikiriEntryCandidates(t.Context(), model.Principal{User: model.User{Role: "user"}}, "game", "", "", 100)
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member candidate inspection = %v", err)
	}
	admin := model.Principal{User: model.User{Role: "admin", Status: "active"}}
	for _, query := range []struct {
		after, prefix string
		limit         int
	}{{"", "", 0}, {"", "", 101}, {strings.Repeat("x", 1025), "", 100}, {"", string([]byte{0xff}), 100}} {
		_, err = s.KirikiriEntryCandidates(t.Context(), admin, "game", query.after, query.prefix, query.limit)
		if !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid bounds %+v = %v", query, err)
		}
	}
}
