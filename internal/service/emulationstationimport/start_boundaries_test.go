package emulationstationimport

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStartRequiresCurrentVersionEvenForExecutedPlan(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"QUEUED", "RUNNING", "COMPLETED", "PARTIAL_FAILURE"} {
		m, sources := startApplicationFixture()
		m.snapshot.Summary.State = state
		result, queued, err := NewStarter(m, sources, time.Now).Start(t.Context(), "import", 3, "editor")
		if !errors.Is(err, ErrVersionConflict) || result.ID != "" || queued || m.writeScopes != 0 || sources.verified {
			t.Fatalf("stale %s result=%#v error=%v", state, result, err)
		}
	}
}

func TestStartChecksEveryIdentityBeforeOpeningWriteScope(t *testing.T) {
	for allowed := range 3 {
		m, sources := startApplicationFixture()
		result, queued, err := func() (Summary, bool, error) {
			uuid.SetRand(&identityEntropy{remaining: allowed})
			defer uuid.SetRand(nil)
			return NewStarter(m, sources, func() time.Time { return time.UnixMilli(10) }).Start(t.Context(), "import", 4, "editor")
		}()
		if result.ID != "" || queued || !errors.Is(err, errIdentityEntropy) || m.writeScopes != 0 {
			t.Fatalf("identity %d queued=%v result=%#v error=%v", allowed, queued, result, err)
		}
	}
}

func TestStartPreservesFinalReadAndResponseFailures(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"current", "response"} {
		m, sources := startApplicationFixture()
		cause := errors.New("start storage failed")
		if stage == "current" {
			m.currentErr = cause
		} else {
			m.responseErr = cause
		}
		result, queued, err := NewStarter(m, sources, func() time.Time { return time.UnixMilli(10) }).Start(t.Context(), "import", 4, "editor")
		if result.ID != "" || queued || !errors.Is(err, cause) {
			t.Fatalf("%s result=%#v error=%v", stage, result, err)
		}
	}
}

func TestStartRevalidatesTargetRootYearAndEvidence(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"target", "root", "year", "evidence"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			m, sources := startApplicationFixture()
			want := ErrSourceChanged
			sources.verify = func() {
				switch field {
				case "target":
					m.snapshot.TargetsValid = false
					want = ErrMappingTargetChanged
				case "root":
					m.snapshot.RootConfigDigest = "changed"
				case "year":
					m.snapshot.ReleaseYearMax++
				case "evidence":
					m.snapshot.Gamelists = append([]GamelistEvidence(nil), m.snapshot.Gamelists...)
					m.snapshot.Gamelists[0].SizeBytes++
				}
			}
			result, queued, err := NewStarter(m, sources, func() time.Time { return time.UnixMilli(10) }).Start(t.Context(), "import", 4, "editor")
			if result.ID != "" || queued || !errors.Is(err, want) || m.queue != nil {
				t.Fatalf("changed %s result=%#v error=%v", field, result, err)
			}
		})
	}
}

func TestStartRejectsVersionOverflowWithoutSourceOrWrite(t *testing.T) {
	t.Parallel()
	m, sources := startApplicationFixture()
	m.snapshot.Summary.Version = math.MaxInt64
	_, queued, err := NewStarter(m, sources, time.Now).Start(t.Context(), "import", math.MaxInt64, "editor")
	if !errors.Is(err, ErrVersionConflict) || queued || sources.verified || m.writeScopes != 0 {
		t.Fatalf("overflow queued=%v error=%v", queued, err)
	}
}

func TestStartEvidenceBoundsPreserveOversizedInvalidFactsOnly(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"empty", "count", "per-file", "total", "null-valid", "null-small", "bad-digest", "oversized-invalid"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			m, sources := startApplicationFixture()
			evidence := m.snapshot.Gamelists[0]
			switch kind {
			case "empty":
				m.snapshot.Gamelists = nil
			case "count":
				m.snapshot.Gamelists = make([]GamelistEvidence, MaxStartGamelists+1)
			case "per-file":
				m.snapshot.Gamelists[0].SizeBytes = MaxStartGamelistBytes + 1
			case "total":
				evidence.SizeBytes = MaxStartGamelistBytes
				m.snapshot.Gamelists = make([]GamelistEvidence, 9)
				for i := range m.snapshot.Gamelists {
					m.snapshot.Gamelists[i] = evidence
				}
			case "null-valid":
				m.snapshot.Gamelists[0].ContentDigest = nil
			case "null-small":
				m.snapshot.Gamelists[0].ContentDigest = nil
				m.snapshot.Gamelists[0].ParseState = "INVALID"
			case "bad-digest":
				m.snapshot.Gamelists[0].ContentDigest = stringPointer("bad")
			case "oversized-invalid":
				m.snapshot.Gamelists = append(m.snapshot.Gamelists, GamelistEvidence{RelativePath: "huge/gamelist.xml", FactsDigest: evidence.FactsDigest, ParseState: "INVALID", SizeBytes: math.MaxInt64})
			}
			_, queued, err := NewStarter(m, sources, func() time.Time { return time.UnixMilli(10) }).Start(t.Context(), "import", 4, "")
			valid := kind == "oversized-invalid"
			if valid != (err == nil) || queued != valid || sources.verified != valid {
				t.Fatalf("%s queued=%v verified=%v error=%v", kind, queued, sources.verified, err)
			}
		})
	}
}
