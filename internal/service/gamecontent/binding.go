package gamecontent

import model "retrom/internal/model/gamecontent"

func replacementBindingMatchesSnapshot(binding model.Binding, snapshot model.JobSnapshot) bool {
	return replacementBindingIdentityOf(binding) == jobSnapshotIdentity(snapshot)
}

type replacementBindingIdentity struct {
	ManifestDigest, InstanceID, PlatformID, CoreID string
	ProviderID, TargetID                           string
	contentPolicyDigest                            string
	VariantID, generation, dependencySHA256        string
	RequirementsSHA256                             string
	Version, PlatformVersion                       int64
}

func replacementBindingIdentityOf(binding model.Binding) replacementBindingIdentity {
	return replacementBindingIdentity{
		ManifestDigest: binding.ManifestDigest, InstanceID: binding.InstanceID, PlatformID: binding.PlatformID,
		CoreID: binding.CoreID, ProviderID: binding.ProviderID, TargetID: binding.TargetID,
		contentPolicyDigest: binding.ContentPolicy.Digest(),
		VariantID:           binding.VariantID, generation: binding.RPGGeneration,
		dependencySHA256: binding.RPGDependencySHA256, RequirementsSHA256: binding.RPGRequirementsSHA256,
		Version: binding.Version, PlatformVersion: binding.PlatformVersion,
	}
}

func jobSnapshotIdentity(snapshot model.JobSnapshot) replacementBindingIdentity {
	return replacementBindingIdentity{
		ManifestDigest: snapshot.BaseManifestDigest, InstanceID: snapshot.PlatformInstanceID,
		PlatformID: snapshot.PlatformID, CoreID: snapshot.CoreID,
		ProviderID: snapshot.ProviderID, TargetID: snapshot.TargetID,
		contentPolicyDigest: snapshot.ContentPolicy.Digest(),
		VariantID:           snapshot.VariantID, generation: snapshot.RPGGeneration,
		dependencySHA256:   snapshot.RPGDependencySHA256,
		RequirementsSHA256: snapshot.RPGRequirementsSHA256,
		Version:            snapshot.GameVersion, PlatformVersion: snapshot.PlatformInstanceVersion,
	}
}
