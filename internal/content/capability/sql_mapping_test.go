package contentcapability

import (
	"database/sql"
	"testing"
)

func TestContentPolicyDoesNotImplementSQLMapping(t *testing.T) {
	if _, ok := any(new(Policy)).(sql.Scanner); ok {
		t.Fatal("domain policy implements database mapping; move the scanner into persistence")
	}
}
