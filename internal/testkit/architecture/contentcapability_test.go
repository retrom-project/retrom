package architecture

import (
	"database/sql"
	"testing"

	"retrom/internal/capability/content/contentcapability"
)

func TestContentPolicyDoesNotImplementSQLMapping(t *testing.T) {
	if _, ok := any(new(contentcapability.Policy)).(sql.Scanner); ok {
		t.Fatal("domain policy implements database mapping; move the scanner into persistence")
	}
}
