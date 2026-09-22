package sourceimport

import (
	"database/sql"
	"testing"
)

func TestRuntimeCheckRejectsMalformedDependencySnapshot(t *testing.T) {
	t.Parallel()
	value, err := projectRuntimeCheck(sql.NullString{String: "BLOCKED", Valid: true}, sql.NullString{String: "LAUNCH_PARENT_MISSING", Valid: true}, sql.NullString{}, sql.NullString{}, sql.NullString{String: `{"dependencies":7}`, Valid: true})
	if value != nil || err == nil {
		t.Fatalf("corrupt dependencies produced a runtime projection: %#v", value)
	}
}
