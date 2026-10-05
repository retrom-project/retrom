package metadatascrape_test

import (
	"errors"
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	metadatapersistence "retrom/internal/persistence/metadatascrape"
	"retrom/internal/service/metadatascrape"

	"retrom/internal/testsupport"
)

func TestScheduleStorageFailureIsNotVersionConflict(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := database.SQL.ExecContext(t.Context(), `DROP TABLE games CASCADE`); err != nil {
		t.Fatal(err)
	}
	_, _, err = metadatascrape.New(metadatapersistence.NewScheduler(database.SQL), nil,
		time.Now).ScheduleGame(t.Context(), "018fbe68-0000-7000-8000-000000000002", 1)
	if err == nil || errors.Is(err, metadatascrape.ErrGameVersionConflict) {
		t.Fatalf("storage failure mapped to version conflict: %v", err)
	}
}
