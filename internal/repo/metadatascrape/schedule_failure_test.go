package metadatascrape_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	metadatapersistence "retrom/internal/repo/metadatascrape"
	"retrom/internal/service/metadatascrape"

	"retrom/internal/testkit/testsupport"
)

func TestScheduleStorageFailureIsNotVersionConflict(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := database.SQL.ExecContext(t.Context(), `DROP TABLE games`); err != nil {
		t.Fatal(err)
	}
	_, _, err = metadatascrape.New(metadatapersistence.NewScheduler(database.SQL), nil, time.Now).ScheduleGame(t.Context(), "game", 1)
	if err == nil || errors.Is(err, metadatascrape.ErrGameVersionConflict) {
		t.Fatalf("storage failure mapped to version conflict: %v", err)
	}
}
