package httpapi

import "testing"

func TestScummVMApprovalProjectionRequiresCurrentExplicitSelection(t *testing.T) {
	evidence := reviewEvidence{validation: reviewValidationResult{canApprove: false}, runtimeScreenshot: optionalReviewProjection{value: map[string]any{"screenshotId": "old-preview"}}}
	if reviewApproval(&evidence, "SCUMMVM_PROJECT") {
		t.Fatal("screenshot bypassed ScummVM selection")
	}
	evidence.validation.canApprove = true
	if !reviewApproval(&evidence, "SCUMMVM_PROJECT") {
		t.Fatal("current selected game blocked")
	}
	evidence.validation.canApprove = false
	if !reviewApproval(&evidence, "ONS_PROJECT") {
		t.Fatal("existing ONS trial approval removed")
	}
}
