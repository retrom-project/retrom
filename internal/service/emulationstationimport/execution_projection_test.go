package emulationstationimport

import "testing"

func TestExecutionProjectionPolicyBelongsToService(t *testing.T) {
	before := newExecutionMemory().before
	change, err := planExecutionFailure(before, ExecutionFailure{Code: "INTERNAL_ERROR", Retryable: true}, 0, 2000)
	if err != nil || !change.ClearScan || change.TerminalItems || change.SchedulePayload {
		t.Fatalf("scan retry=%#v %v", change, err)
	}
	before.Kind = "SERVER_EMULATIONSTATION_IMPORT"
	change, err = planExecutionFailure(before, ExecutionFailure{Code: "INTERNAL_ERROR"}, 1, 2000)
	if err != nil || change.ClearScan || !change.TerminalItems || !change.SchedulePayload || !change.RetryFailedItems {
		t.Fatalf("import failure=%#v %v", change, err)
	}
}
