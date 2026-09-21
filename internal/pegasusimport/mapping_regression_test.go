package pegasusimport

import (
	"errors"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func TestMappingReadFailurePreservesDatabaseCause(t *testing.T) {
	t.Parallel()
	db := newPegasusRetryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `ALTER TABLE pegasus_imports RENAME TO unavailable_pegasus_plans`); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
	value, err := service.UpdateMappings(t.Context(), "import", 4, []Mapping{{CollectionID: "019b0000-0000-7000-8000-000000000001", Action: "SKIP", TagIDs: []string{}}})
	var databaseError *sqlite.Error
	if !errors.As(err, &databaseError) || value.ID != "" {
		t.Fatalf("mapping storage failure masked: %#v, %v", value, err)
	}
}
