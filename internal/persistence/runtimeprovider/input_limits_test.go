package runtimeprovider

import (
	"testing"
	"time"

	contentcapability "retrom/internal/content/capability"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/contentquery"
	service "retrom/internal/service/runtimeprovider"
)

func TestProviderUpgradeReplacesInputLimitsAndValidationIdentity(t *testing.T) {
	db := openProjectionDatabase(t)
	read := func() contentcapability.Policy {
		t.Helper()
		var policy contentcapability.Policy
		if err := dbapi.QueryRowContext(t.Context(), db.SQL, `SELECT `+contentquery.BindingPolicySQL+` FROM runtime_target_bindings binding WHERE binding_id='fixture-target'`).Scan(contentquery.ScanPolicy(&policy)); err != nil {
			t.Fatal(err)
		}
		return policy
	}
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	maximum := int64(10)
	initial.Providers[0].Targets[0].Target.Inputs[0].MaxFileBytes = &maximum
	if err := service.New(New(db.SQL)).Reconcile(t.Context(), initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	before := read()
	if before.MaxFileBytes("SINGLE_FILE") != 10 {
		t.Fatalf("policy=%+v", before)
	}
	upgrade := projectionFixture("1.1.0", "b", []string{"state-v1"})
	if err := service.New(New(db.SQL)).Reconcile(t.Context(), upgrade, time.UnixMilli(2)); err != nil {
		t.Fatal(err)
	}
	after := read()
	if after.MaxFileBytes("SINGLE_FILE") != 0 || before.DigestFor("SINGLE_FILE") == after.DigestFor("SINGLE_FILE") {
		t.Fatalf("obsolete policy retained: %+v", after)
	}
}
