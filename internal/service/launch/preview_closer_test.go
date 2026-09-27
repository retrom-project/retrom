package launch

import (
	"context"
	"errors"
	"testing"
)

type previewCloseMemory struct {
	source  PreviewCloseSource
	found   bool
	writes  int
	failure error
}

func (m *previewCloseMemory) WithPreviewClose(_ context.Context, work func(PreviewCloseScope) error) error {
	return work(m)
}

func (m *previewCloseMemory) Preview(context.Context, string) (PreviewCloseSource, bool, error) {
	return m.source, m.found, nil
}

func (m *previewCloseMemory) Finish(context.Context, PreviewCloseSource, int64) error {
	m.writes++
	return m.failure
}

func TestPreviewCloseIsAuthorizedAndIdempotent(t *testing.T) {
	for _, state := range []string{"CREATED", "ACTIVE", "FINISHED", "EXPIRED", "REVOKED"} {
		t.Run(state, func(t *testing.T) {
			m := &previewCloseMemory{found: true, source: PreviewCloseSource{ID: "preview", Version: 1, Session: SessionRecord{State: state, CredentialHash: []byte("hash"), HardExpiresAtMS: 1_000_000}}}
			c := NewPreviewCloser(m, playClock, matchTestCapability)
			if err := c.Finish(t.Context(), "preview", "wrong"); !errors.Is(err, ErrCredential) || m.writes != 0 {
				t.Fatalf("unauthorized close: %v", err)
			}
			err := c.Finish(t.Context(), "preview", "valid")
			want := 0
			if state == "ACTIVE" || state == "CREATED" {
				want = 1
			}
			blocked := state == "EXPIRED" || state == "REVOKED"
			if (err != nil) != blocked || m.writes != want {
				t.Fatalf("state=%s error=%v writes=%d", state, err, m.writes)
			}
			m.source.Session.HardExpiresAtMS = playClock().UnixMilli()
			if err := c.Finish(t.Context(), "preview", "valid"); !errors.Is(err, ErrCredential) {
				t.Fatalf("expired capability: %v", err)
			}
		})
	}
}
