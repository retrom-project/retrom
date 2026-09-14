//go:build integration

package libraryimport

import (
	"context"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/repo/dbexec"
	librarypersistence "retrom/internal/repo/libraryimport"
	"retrom/internal/service/libraryimport"
)

type draftDependencyState struct {
	tracked       bool
	replaceBundle bool
	snapshotJSON  string
	status        string
	code          string
	dependencies  []corevalidation.BIOSDependency
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
