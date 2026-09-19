package architecture

import (
	"errors"
	"strings"
	"testing"
)

func TestArchiveResourceRejectsIntegrationOnlyBadBinding(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "", "//go:build !integration\n\n" + archiveAdapterFixture},
		archiveFixtureEdit{
			"internal/adapter/archive/archive_integration.go", "",
			"//go:build integration\n\n" + archiveAdapterFixture + "\nfunc (*cursor) Apply() error { return nil }\n",
		},
	)
	ordinary, normalViolations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, ordinary, normalViolations)
	tagged, taggedViolations := inspectArchiveFixture(t, root, owners, "integration")
	requireArchiveRejected(t, tagged, taggedViolations, "hidden extra resource method")
	merged := mergePortBuilds(append(ordinary, tagged...))
	port := findImplementationPort(t, merged, "(retrom/internal/model/libraryimport.Archives).Open")
	if len(port.ArchiveResources) != 2 || port.ArchiveResources[0].Status == port.ArchiveResources[1].Status {
		t.Fatalf("valid build masked the failed binding: %+v", port.ArchiveResources)
	}
}

func TestArchiveResourceEvidenceDoesNotSurviveChangedSource(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	proof := requireArchiveProof(t, ports, violations)
	writeInventoryFile(t, root, "internal/adapter/archive/archive.go",
		archiveAdapterFixture+"\nfunc (*cursor) Publish() error { return nil }\n")
	if err := verifyUnchangedSources(root, SourceSnapshot{Files: proof.Sources}); err == nil {
		t.Fatal("binding evidence survived a changed concrete method set")
	}
}

func TestArchiveResourceRejectsUnknownOwnershipAndCompilation(t *testing.T) {
	t.Parallel()
	t.Run("unregistered adapter", func(t *testing.T) {
		root, owners := newArchiveFixture(t)
		for index := range owners.Packages {
			if owners.Packages[index].Layer == "adapter" {
				owners.Packages[index].Layer = ""
			}
		}
		ports, violations := inspectArchiveFixture(t, root, owners, "default")
		requireArchiveRejected(t, ports, violations, "no local production definition")
	})
	t.Run("compile error", func(t *testing.T) {
		root, _ := newArchiveFixture(t,
			archiveFixtureEdit{"internal/adapter/archive/archive.go", "type cursor struct{}", "type cursor struct { hidden Missing }"},
		)
		if _, err := loadInventoryGraph(t.Context(), root, []string{"./..."}, "default"); !errors.Is(err, ErrTypeAnalysis) {
			t.Fatalf("incomplete graph was accepted: %v", err)
		}
	})
}

func TestArchiveResourceDoesNotExemptServiceReexports(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/service/libraryimport/service.go", "", archiveServiceFixture +
			"\ntype ResourceAlias = model.StreamCursor\n"},
	)
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	_, violations := inspectReexports(root, graph, owners)
	for _, violation := range violations {
		if violation.Rule == "AR08" && strings.Contains(violation.Symbol, "ResourceAlias") {
			return
		}
	}
	t.Fatalf("resource profile bypassed the existing Service reexport rule: %+v", violations)
}

func TestArchiveResourceKeepsFiniteFailureBranches(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "func openCursor(context.Context)", "func openCursor(ctx context.Context)"},
		archiveFixtureEdit{
			"internal/adapter/archive/archive.go", "cursor := &cursor{}",
			"err := ctx.Err(); if err != nil { return nil, err }; cursor := &cursor{}",
		},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveProof(t, ports, violations)
}

func TestArchiveResourceRejectsMutableFailureEvidence(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "func openCursor(context.Context)", "func openCursor(ctx context.Context)"},
		archiveFixtureEdit{
			"internal/adapter/archive/archive.go", "cursor := &cursor{}",
			"err := ctx.Err(); if err != nil { err = nil; return nil, err }; cursor := &cursor{}",
		},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveRejected(t, ports, violations, "nil resource without a proven failure")
}

func TestArchiveResourceRejectsCapturedFailureMutation(t *testing.T) {
	t.Parallel()
	root, owners := newArchiveFixture(t,
		archiveFixtureEdit{"internal/adapter/archive/archive.go", "func openCursor(context.Context)", "func openCursor(ctx context.Context)"},
		archiveFixtureEdit{
			"internal/adapter/archive/archive.go", "cursor := &cursor{}",
			"err := ctx.Err(); if err != nil { func(){err = nil}(); return nil, err }; cursor := &cursor{}",
		},
	)
	ports, violations := inspectArchiveFixture(t, root, owners, "default")
	requireArchiveRejected(t, ports, violations, "nil resource without a proven failure")
}
