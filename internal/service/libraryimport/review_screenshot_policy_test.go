package libraryimport

import (
	"context"
	"strings"
	"testing"

	"retrom/internal/core/scummvm"
)

type screenshotRPGProfile struct{ profile RPGReviewProfile }

func (reader screenshotRPGProfile) Profile(context.Context, string) (RPGReviewProfile, bool, error) {
	return reader.profile, true, nil
}

func TestScreenshotApprovalRetainsRPGDependencyDiagnosis(t *testing.T) {
	profile := RPGReviewProfile{Generation: "RPG2003", AnalysisJSON: `{"selfContained":false}`}
	dependencies, err := ResolveRPGReviewDependencies(profile)
	if err != nil || dependencies.Status != "BLOCKED" {
		t.Fatalf("fixture dependencies: %+v %v", dependencies, err)
	}
	screenshot := "screenshot"
	run := reviewApprovalRun{
		ctx: t.Context(), scope: ReviewApprovalScope{Profiles: screenshotRPGProfile{profile}},
		head: ReviewApprovalHead{
			PlatformID: "rpgmaker", ValidationStatus: "BLOCKED",
			ScreenshotID: &screenshot, DependencyJSON: dependencies.SnapshotJSON,
		},
	}
	if err := run.prepareValidation(); err != nil {
		t.Fatal(err)
	}
	if !run.screenshotOverride || run.head.ValidationStatus != "BLOCKED" ||
		run.head.DependencyJSON != dependencies.SnapshotJSON || run.runtimeDependencyJSON != dependencies.SnapshotJSON ||
		run.rpgDependencies != dependencies || run.rpgProfile.SelfContainedOverride {
		t.Fatal("screenshot approval rewrote diagnostic facts or fabricated self-contained confirmation")
	}
	run.head.ScreenshotID = nil
	if err := run.prepareValidation(); err == nil {
		t.Fatal("missing screenshot bypassed the unresolved RTP declaration")
	}
}

func TestScreenshotApprovalPreservesScummVMDiagnostics(t *testing.T) {
	screenshot := "screenshot"
	raw := `{"schemaVersion":1,"kind":"SCUMMVM","detection":{"sourceDigest":"` + strings.Repeat("a", 64) + `","upstreamCommit":"` + strings.Repeat("b", 40) + `","roots":[],"candidates":[]},"selectedCandidateId":""}`
	if _, err := scummvm.ParseSnapshot(raw); err != nil {
		t.Fatalf("invalid diagnosis fixture: %v", err)
	}
	run := reviewApprovalRun{ctx: t.Context(), head: ReviewApprovalHead{
		ContentKind: "SCUMMVM_PROJECT", ValidationStatus: "BLOCKED", ScreenshotID: &screenshot, DependencyJSON: raw,
	}}
	if err := run.prepareValidation(); err != nil {
		t.Fatal(err)
	}
	if !run.screenshotOverride || run.head.ValidationStatus != "BLOCKED" || run.runtimeDependencyJSON != raw {
		t.Fatal("screenshot approval replaced the original ScummVM diagnosis")
	}
}
