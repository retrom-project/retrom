package libraryimport

import "testing"

func TestScummVMApprovalProjectionRequiresCurrentExplicitSelection(t *testing.T) {
	selectedReady := false
	if ReviewApproval("SCUMMVM_PROJECT", selectedReady, true) {
		t.Fatal("screenshot bypassed ScummVM selection")
	}
	selectedReady = true
	if !ReviewApproval("SCUMMVM_PROJECT", selectedReady, true) {
		t.Fatal("current selected game blocked")
	}
	selectedReady = false
	if !ReviewApproval("ONS_PROJECT", selectedReady, true) {
		t.Fatal("existing ONS trial approval removed")
	}
}
