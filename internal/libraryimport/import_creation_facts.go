package libraryimport

import application "retrom/internal/service/libraryimport"

// The synchronous preparation engine retains its private representation until
// its full materialization use case moves behind the application boundary.
func importTargetFacts(target creationTarget) application.ImportTarget {
	return application.ImportTarget{
		PlatformID: target.platformID, DefaultCoreID: target.defaultCoreID,
		CoreID: target.coreID, BindingID: target.bindingID, ProviderID: target.providerID, TargetID: target.targetID,
		DeliveryProfile: target.deliveryProfile, Version: target.instanceVersion, Policy: target.contentPolicy,
	}
}

func legacyCreationTarget(target application.ImportTarget) creationTarget {
	return creationTarget{
		platformID: target.PlatformID, defaultCoreID: target.DefaultCoreID,
		coreID: target.CoreID, bindingID: target.BindingID, providerID: target.ProviderID, targetID: target.TargetID,
		deliveryProfile: target.DeliveryProfile, instanceVersion: target.Version, contentPolicy: target.Policy,
	}
}

func importFileFacts(files []importSourceFile) []application.ImportFile {
	result := make([]application.ImportFile, 0, len(files))
	for _, file := range files {
		result = append(result, application.ImportFile{
			ID: file.id, Path: file.path, BlobID: file.blobID, SHA256: file.sha256, Size: file.size,
		})
	}
	return result
}
