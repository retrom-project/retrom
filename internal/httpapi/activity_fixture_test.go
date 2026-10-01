package httpapi

import (
	"testing"

	dbapi "retrom/internal/database"
)

// Direct SQL history fixtures must seed both current models. Product tests use
// the play snapshot endpoint, which maintains them atomically.
func seedPlayActivity(t *testing.T, database dbapi.Executor) {
	t.Helper()
	mustExecHTTPTest(t, database, `INSERT INTO profile_game_activity
(profile_id,game_id,last_played_at_ms,active_duration_ms,session_count)
SELECT profile_id,game_id,max(started_at_ms),sum(active_duration_ms),count(*)
FROM play_sessions WHERE 1 GROUP BY profile_id,game_id
ON CONFLICT(profile_id,game_id) DO UPDATE SET last_played_at_ms=excluded.last_played_at_ms,
active_duration_ms=excluded.active_duration_ms,session_count=excluded.session_count`)
}
