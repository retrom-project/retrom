package pegasusimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type startMemory struct {
	snapshot                     StartSnapshot
	readErr, writeErr, commitErr error
	queue                        *StartPlan
	onWrite                      func()
	writeScopes                  int
}

func (m *startMemory) Inspect(context.Context, string) (StartSnapshot, error) {
	return m.snapshot, m.readErr
}

func (m *startMemory) WithStart(_ context.Context, work func(StartScope) error) error {
	m.writeScopes++
	if m.onWrite != nil {
		m.onWrite()
	}
	if err := work(StartScope{Read: m, Write: m}); err != nil {
		return err
	}
	return m.commitErr
}

func (m *startMemory) Current(context.Context, string) (StartSnapshot, error) {
	return m.snapshot, m.readErr
}

func (m *startMemory) Queue(_ context.Context, plan StartPlan) error {
	m.queue = &plan
	m.snapshot.Summary.State = "QUEUED"
	return m.writeErr
}

type startSources struct {
	root     SelectedRoot
	err      error
	verified bool
	verify   func()
}

func (s *startSources) Select(context.Context, string, string) (SelectedRoot, error) {
	return s.root, s.err
}

func (s *startSources) VerifyMetadata(context.Context, string, string, []MetadataEvidence) error {
	s.verified = true
	if s.verify != nil {
		s.verify()
	}
	return s.err
}

func startApplicationFixture() (*startMemory, *startSources) {
	return &startMemory{snapshot: StartSnapshot{Summary: Summary{ID: "import", State: "AWAITING_MAPPING", Version: 4, ExpiresAtMS: 100, CreatedBy: CreatedBy{ID: "actor"}, Root: RootRef{ID: "root"}, SourceRelativePath: "Roms", Counts: Counts{Collections: 2, MappedCollections: 1, SkippedCollections: 1}}, RootConfigDigest: "digest", SourceSnapshotDigest: "snapshot", TagsValid: true, Metadata: []MetadataEvidence{{RelativePath: "metadata.pegasus.txt", SizeBytes: 10, ContentDigest: "content", FactsDigest: "facts"}}}}, &startSources{root: SelectedRoot{ID: "root", Digest: "digest"}}
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
		result, queued, err := NewStarter(m, sources, time.Now).Start(t.Context(), "import", 1, "editor")
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
			want := ErrVersionConflict
			sources.verify = func() {
				switch change {
				case "expiry":
					now = 100
					want = ErrExpired
				case "version":
					m.snapshot.Summary.Version++
				case "tags":
					m.snapshot.TagsValid = false
					want = ErrMapping
				case "capacity":
					m.snapshot.OtherActive = true
					want = ErrActive
				case "digest":
					m.snapshot.SourceSnapshotDigest = "changed"
					want = ErrSourceChanged
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
	if !errors.Is(err, ErrSourceChanged) || result.ID != "" || queued || sources.verified || m.writeScopes != 0 {
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
