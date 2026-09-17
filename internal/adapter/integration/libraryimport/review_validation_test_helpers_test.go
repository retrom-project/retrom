package libraryimport

import "retrom/internal/model/libraryimport"

// These small fixtures keep policy assertions close to their tests after the
// production validation workflow moved into service/libraryimport. They are
// deliberately test-only and do not provide a repo-to-service callback path.
type rpgReviewBinding struct {
	generation string
	override   bool
	analysis   libraryimport.RPGReviewAnalysis
}

type rpgDependencyState struct {
	snapshotJSON string
	status       string
	code         string
}

func resolveRPGDependencies(profile rpgReviewBinding) (rpgDependencyState, string) {
	resolved := libraryimport.ResolveRPGResourcePolicy(profile.generation, profile.override, profile.analysis)
	return rpgDependencyState{
		status: resolved.Status, code: resolved.Code,
		snapshotJSON: resolved.SnapshotJSON,
	}, resolved.Digest
}
