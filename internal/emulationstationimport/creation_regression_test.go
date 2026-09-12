package emulationstationimport

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

var errCreationEntropy = errors.New("creation entropy unavailable")

type creationEntropyFailure struct{}

func (creationEntropyFailure) Read([]byte) (int, error) { return 0, errCreationEntropy }

func TestCreationRejectsUnavailableIdentitiesBeforeWriting(t *testing.T) {
	fixture := newLifecycleFixture(t)
	var value Summary
	var err error
	func() {
		uuid.SetRand(creationEntropyFailure{})
		defer uuid.SetRand(nil)
		value, err = fixture.service.Create(fixture.context, CreateRequest{RootID: "games"}, fixture.userID)
	}()
	if value.ID != "" || !errors.Is(err, errCreationEntropy) {
		t.Fatalf("failed identity created plan=%#v error=%v", value, err)
	}
	var count int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM emulationstation_imports`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed identity persisted %d plans", count)
	}
}

func TestCreationChecksCapacityInFinalWriteTransaction(t *testing.T) {
	fixture := newLifecycleFixture(t)
	request := CreateRequest{RootID: "games"}
	for range 19 {
		if _, err := fixture.service.Create(fixture.context, request, fixture.userID); err != nil {
			t.Fatal(err)
		}
	}
	other := &Service{database: fixture.database, roots: fixture.service.roots, tags: fixture.service.tags, now: func() time.Time { return *fixture.now }, wake: make(chan struct{}, 1)}
	inserted := false
	fixture.service.now = func() time.Time {
		if !inserted {
			inserted = true
			if _, err := other.Create(fixture.context, request, fixture.userID); err != nil {
				t.Fatal(err)
			}
		}
		return *fixture.now
	}
	value, err := fixture.service.Create(fixture.context, request, fixture.userID)
	if value.ID != "" || !errors.Is(err, ErrActive) {
		t.Fatalf("capacity race created plan=%#v error=%v", value, err)
	}
	var count int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM emulationstation_imports`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 20 {
		t.Fatalf("capacity race retained %d plans", count)
	}
}

func TestCreationFreezesReleaseYearWithCreationTime(t *testing.T) {
	fixture := newLifecycleFixture(t)
	before := time.Date(2026, time.December, 31, 23, 59, 59, 999000000, time.UTC)
	reads := 0
	fixture.service.now = func() time.Time {
		reads++
		if reads == 1 {
			return before
		}
		return before.Add(time.Millisecond)
	}
	value, err := fixture.service.Create(fixture.context, CreateRequest{RootID: "games"}, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	var year int
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT release_year_max FROM emulationstation_imports WHERE id=?`, value.ID).Scan(&year); err != nil {
		t.Fatal(err)
	}
	if year != before.UTC().Year()+1 || value.CreatedAtMS != before.UnixMilli() {
		t.Fatalf("inconsistent frozen creation time=%d releaseYearMax=%d", value.CreatedAtMS, year)
	}
}
