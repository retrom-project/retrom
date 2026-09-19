package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type startMemory struct {
	snapshot                     model.StartSnapshot
	readErr, writeErr, commitErr error
	queue                        *model.StartPlan
	onWrite                      func()
	writeScopes                  int
	currentErr, responseErr      error
}

func (m *startMemory) Inspect(context.Context, string) (model.StartSnapshot, error) {
	return m.snapshot, m.readErr
}

func (m *startMemory) WithStart(_ context.Context, work func(model.StartScope) error) error {
	m.writeScopes++
	if m.onWrite != nil {
		m.onWrite()
	}
	if err := work(model.StartScope{Payload: emptyPayloadScope(), Read: m, Write: m}); err != nil {
		return err
	}
	return m.commitErr
}

func (m *startMemory) Current(context.Context, string) (model.StartSnapshot, error) {
	if m.currentErr != nil {
		return model.StartSnapshot{}, m.currentErr
	}
	if m.queue != nil && m.responseErr != nil {
		return model.StartSnapshot{}, m.responseErr
	}
	return m.snapshot, m.readErr
}

func (m *startMemory) Queue(_ context.Context, plan model.StartPlan) error {
	m.queue = &plan
	m.snapshot.Summary.State = "QUEUED"
	return m.writeErr
}

type startSources struct {
	root     model.SelectedRoot
	err      error
	verified bool
	verify   func()
}

func (s *startSources) Select(context.Context, string, string) (model.SelectedRoot, error) {
	return s.root, s.err
}

func (s *startSources) VerifyGamelists(context.Context, string, string, []model.GamelistEvidence) error {
	s.verified = true
	if s.verify != nil {
		s.verify()
	}
	return s.err
}

func startApplicationFixture() (*startMemory, *startSources) {
	return &startMemory{snapshot: model.StartSnapshot{Summary: model.Summary{ID: "import", State: "AWAITING_MAPPING", Version: 4, ExpiresAtMS: 100, CreatedBy: model.CreatedBy{ID: "actor"}, Root: model.RootRef{ID: "root"}, SourceRelativePath: "Roms", Counts: model.Counts{Collections: 2, MappedCollections: 1, SkippedCollections: 1}}, TagsValid: true, TargetsValid: true, FrozenSourceSnapshot: model.FrozenSourceSnapshot{RootConfigDigest: "digest", SourceSnapshotDigest: "snapshot", ReleaseYearMax: 2027, Gamelists: []model.GamelistEvidence{{RelativePath: "gamelist.xml", SizeBytes: 10, ContentDigest: stringPointer("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"), FactsDigest: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", ParseState: "VALID"}}}}}, &startSources{root: model.SelectedRoot{ID: "root", Digest: "digest"}}
}

func TestStartVerifiesSourcesBeforeAtomicQueue(t *testing.T) {
	t.Parallel()
	m, sources := startApplicationFixture()
	m.onWrite = func() {
		if !sources.verified {
			t.Fatal("source verification ran in write scope")
		}
	}
	result, queued, err := NewStarter(m, sources, func() time.Time { return time.UnixMilli(10) }).Start(t.Context(), "import", 4, "editor")
	if err != nil {
		t.Fatal(err)
	}
	if !queued || result.State != "QUEUED" || m.queue == nil {
		t.Fatalf("start: %#v queued=%v plan=%#v", result, queued, m.queue)
	}
	if m.queue.JobID == "" || m.queue.ExecutionID == "" || m.queue.AuditID == "" || m.queue.NowMS != 10 || m.queue.Before.Summary.Version != 4 {
		t.Fatalf("start plan: %#v", m.queue)
	}
}

func TestStartAlreadyExecutedIsIdempotentWithoutSourceAccess(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"QUEUED", "RUNNING", "COMPLETED", "PARTIAL_FAILURE"} {
		m, sources := startApplicationFixture()
		m.snapshot.Summary.State = state
		sources.err = errors.New("source unavailable")
		result, queued, err := NewStarter(m, sources, time.Now).Start(t.Context(), "import", 4, "editor")
		if err != nil || queued || result.State != state || m.writeScopes != 0 {
			t.Fatalf("existing start %s: %#v queued=%v %v", state, result, queued, err)
		}
	}
}

func TestStartRechecksExpiryVersionTagsAndCapacityAfterSourceVerification(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"expiry", "version", "tags", "capacity", "digest"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			m, sources := startApplicationFixture()
			now := int64(10)
			want := model.ErrVersionConflict
			sources.verify = func() {
				switch change {
				case "expiry":
					now = 100
					want = model.ErrExpired
				case "version":
					m.snapshot.Summary.Version++
				case "tags":
					m.snapshot.TagsValid = false
					want = model.ErrMapping
				case "capacity":
					m.snapshot.OtherActive = true
					want = model.ErrActive
				case "digest":
					m.snapshot.SourceSnapshotDigest = "changed"
					want = model.ErrSourceChanged
				}
			}
			result, queued, err := NewStarter(m, sources, func() time.Time { return time.UnixMilli(now) }).Start(t.Context(), "import", 4, "editor")
			if !errors.Is(err, want) || result.ID != "" || queued || m.queue != nil {
				t.Fatalf("changed %s queued: %#v %v", change, result, err)
			}
		})
	}
}

func TestStartRejectsRootDigestDriftBeforeReadingMetadata(t *testing.T) {
	t.Parallel()
	m, sources := startApplicationFixture()
	sources.root.Digest = "changed"
	result, queued, err := NewStarter(m, sources, func() time.Time { return time.UnixMilli(10) }).Start(t.Context(), "import", 4, "editor")
	if !errors.Is(err, model.ErrSourceChanged) || result.ID != "" || queued || sources.verified || m.writeScopes != 0 {
		t.Fatalf("changed root accepted: %#v %v", result, err)
	}
}

func TestStartPreservesFailureCausesWithoutPartialResponse(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"read", "source", "write", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			m, sources := startApplicationFixture()
			cause := errors.New("start failure")
			switch phase {
			case "read":
				m.readErr = cause
			case "source":
				sources.err = cause
			case "write":
				m.writeErr = cause
			case "commit":
				m.commitErr = cause
			}
			result, queued, err := NewStarter(m, sources, func() time.Time { return time.UnixMilli(10) }).Start(t.Context(), "import", 4, "editor")
			if !errors.Is(err, cause) || result.ID != "" || queued {
				t.Fatalf("partial start %s: %#v %v", phase, result, err)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }
