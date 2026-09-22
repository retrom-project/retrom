package store

import (
	"path/filepath"
	"testing"
	"time"
)

type applicationFixture struct {
	database *DB
	userID   string
}

func openApplicationFixture(t *testing.T) applicationFixture {
	t.Helper()
	database, err := Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	userID := "00000000-0000-7000-8000-000000000010"
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile-schema','Schema Admin',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'profile-schema','schema-admin','Schema Admin','ADMIN','ENABLED',1,1)`, userID)
	if err != nil {
		t.Fatal(err)
	}
	return applicationFixture{database, userID}
}
