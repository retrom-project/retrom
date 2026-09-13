package mediaaccess

import (
	"context"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	service "retrom/internal/service/mediaaccess"
	"retrom/internal/testsupport"
)

func TestMediaSnapshotsPreserveStorageAndCancellationCauses(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.Date(2028, 3, 4, 5, 6, 7, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, source := range []string{"game_assets", "save_states", "scrape_candidate_assets", "pegasus_import_item_assets"} {
		t.Run(source, func(t *testing.T) {
			cause := errors.New("media snapshot unavailable")
			hits := 0
			fault := testsupport.OpenSQLFaultDatabase(t, database.SQL, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
					if strings.Contains(query, "FROM "+source) && len(args) > 0 && args[0].Value == "asset" {
						hits++
						return cause
					}
					return nil
				},
			})
			access := service.New(New(fault))
			var err error
			switch source {
			case "game_assets":
				_, err = access.Game(t.Context(), "asset")
			case "save_states":
				_, err = access.Save(t.Context(), "asset", "profile")
			default:
				_, err = access.Review(t.Context(), "asset", "COVER")
			}
			if !errors.Is(err, cause) || errors.Is(err, service.ErrNotFound) || hits != 1 {
				t.Fatalf("cause lost: %v hits=%d", err, hits)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.New(New(database.SQL)).Game(ctx, "asset"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation cause lost: %v", err)
	}
}
