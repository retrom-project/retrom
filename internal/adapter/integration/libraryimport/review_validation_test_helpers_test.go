package libraryimport

import (
	"context"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/repo/dbexec"
	librarypersistence "retrom/internal/repo/libraryimport"
	"retrom/internal/service/libraryimport"
)

// These small fixtures keep policy assertions close to their tests after the
// production validation workflow moved into service/libraryimport. They are
// deliberately test-only and do not provide a repo-to-service callback path.
type rpgReviewBinding struct {
	generation       string
	override         bool
	dependencySHA256 string
	analysis         libraryimport.RPGReviewAnalysis
}

type draftDependencyState struct {
	tracked       bool
	replaceBundle bool
	snapshotJSON  string
	status        string
	code          string
	dependencies  []corevalidation.BIOSDependency
}

func resolveRPGDependencies(profile rpgReviewBinding) (draftDependencyState, string) {
	resolved := libraryimport.ResolveRPGResourcePolicy(profile.generation, profile.override, profile.analysis)
	return draftDependencyState{
		tracked: true, status: resolved.Status, code: resolved.Code,
		snapshotJSON: resolved.SnapshotJSON,
	}, resolved.Digest
}

func resolveArcadeDraftBIOSState(
	ctx context.Context,
	transaction dbexec.Executor,
	providerID, targetID, previousSnapshot, previousStatus, previousCode string,
) (draftDependencyState, error) {
	resolved, err := libraryimport.ResolveCreationArcade(ctx, librarypersistence.BindCreationArcade(transaction),
		providerID, targetID, previousSnapshot, previousStatus, previousCode)
	if err != nil {
		return draftDependencyState{}, err
	}
	return draftDependencyState{
		tracked: resolved.Tracked, status: resolved.Status, code: resolved.Code,
		snapshotJSON: resolved.SnapshotJSON, dependencies: resolved.Dependencies,
	}, nil
}
