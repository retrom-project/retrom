package runtimebundle

import (
	"errors"
	"regexp"

	runtimejson "retrom/internal/capability/runtime/runtimejson"
	runtimecontract "retrom/internal/model/runtimecontract"
)

var (
	ErrActiveInvalid  = errors.New("RUNTIME_PROVIDER_ACTIVE_INVALID")
	commitPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

const providerRepository = "https://github.com/retrom-project/retrom-runtime"

func ParseActiveDescriptor(contents []byte) (runtimecontract.ActiveDescriptor, error) {
	if !validActiveRawShape(contents) {
		return runtimecontract.ActiveDescriptor{}, ErrActiveInvalid
	}
	var result runtimecontract.ActiveDescriptor
	if err := decodeClosed(contents, &result); err != nil || !validActive(result) {
		return runtimecontract.ActiveDescriptor{}, ErrActiveInvalid
	}
	return result, nil
}

func validActiveRawShape(contents []byte) bool {
	value, err := runtimejson.ParseStrictJSON(contents)
	active, ok := value.(map[string]any)
	if err != nil || !ok ||
		!runtimejson.ExactMap(active, "schemaVersion", "source", "sourceTreeSha256", "release", "providers") ||
		!validActiveRawRelease(active["release"]) {
		return false
	}
	providers, ok := active["providers"].([]any)
	if !ok {
		return false
	}
	for _, providerValue := range providers {
		if !validActiveRawProvider(providerValue) {
			return false
		}
	}
	return true
}

func validActiveRawRelease(value any) bool {
	if value == nil {
		return true
	}
	release, ok := value.(map[string]any)
	return ok && runtimejson.ExactMap(release, "repository", "tag", "commit")
}

func validActiveRawProvider(value any) bool {
	provider, ok := value.(map[string]any)
	if !ok || !runtimejson.ExactMap(provider,
		"providerId", "providerVersion", "providerApiVersion", "bundleSha256", "bundleSizeBytes",
		"manifestSha256", "moduleSha256", "clientModulePath", "installationPath", "fileCount",
		"unpackedSizeBytes", "targets") {
		return false
	}
	targets, ok := provider["targets"].([]any)
	if !ok {
		return false
	}
	for _, target := range targets {
		if !validActiveRawTarget(target) {
			return false
		}
	}
	return true
}

func validActiveRawTarget(value any) bool {
	target, ok := value.(map[string]any)
	if !ok || !runtimejson.ExactMap(target, "id", "checkpoint") {
		return false
	}
	if target["checkpoint"] == nil {
		return true
	}
	checkpoint, ok := target["checkpoint"].(map[string]any)
	return ok && validCheckpointShape(checkpoint)
}

func validActive(value runtimecontract.ActiveDescriptor) bool {
	if value.SchemaVersion != 1 || len(value.Providers) == 0 {
		return false
	}
	switch value.Source {
	case "candidate":
		if value.Release != nil || value.SourceTreeSHA256 == nil || !digestPattern(*value.SourceTreeSHA256) {
			return false
		}
	case "production":
		if value.SourceTreeSHA256 != nil || value.Release == nil || !validRelease(*value.Release) {
			return false
		}
	default:
		return false
	}
	previous := ""
	for _, provider := range value.Providers {
		if !validActiveProvider(provider) || previous != "" && previous >= provider.ProviderID {
			return false
		}
		previous = provider.ProviderID
	}
	return true
}

func validRelease(value runtimecontract.ReleaseIdentity) bool {
	return value.Repository == providerRepository && releaseTagPattern.MatchString(value.Tag) &&
		commitPattern.MatchString(value.Commit)
}

func validActiveProvider(value runtimecontract.ActiveProvider) bool {
	if !validActiveProviderIdentity(value) || !validActiveProviderSize(value) || len(value.Targets) == 0 {
		return false
	}
	previous := ""
	for _, target := range value.Targets {
		if !validActiveTarget(target) || previous != "" && previous >= target.ID {
			return false
		}
		previous = target.ID
	}
	return true
}

func validActiveProviderIdentity(value runtimecontract.ActiveProvider) bool {
	return identityPattern.MatchString(value.ProviderID) && semverPattern.MatchString(value.ProviderVersion) &&
		value.ProviderAPI == 1 && digestPattern(value.BundleSHA256) && digestPattern(value.ManifestSHA256) &&
		digestPattern(value.ModuleSHA256) && value.ClientModulePath == "client.mjs" &&
		value.InstallationPath == value.ProviderID+"/"+value.BundleSHA256
}

func validActiveProviderSize(value runtimecontract.ActiveProvider) bool {
	return positiveSafe(value.BundleSizeBytes) && value.FileCount >= 3 && value.FileCount <= 100000 &&
		positiveSafe(value.UnpackedSizeBytes)
}

func validActiveTarget(value runtimecontract.ActiveTarget) bool {
	return identityPattern.MatchString(value.ID) &&
		(value.Checkpoint == nil || validCheckpoint(*value.Checkpoint))
}

func positiveSafe(value int64) bool {
	return value > 0 && value <= 9007199254740991
}
