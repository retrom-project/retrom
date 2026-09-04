package runtimebundle

import (
	"errors"
	"regexp"
)

var (
	ErrActiveInvalid  = errors.New("RUNTIME_PROVIDER_ACTIVE_INVALID")
	commitPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

const providerRepository = "https://github.com/retrom-project/retrom-runtime"

type ActiveDescriptor struct {
	SchemaVersion    int              `json:"schemaVersion"`
	Source           string           `json:"source"`
	SourceTreeSHA256 *string          `json:"sourceTreeSha256"`
	Release          *ReleaseIdentity `json:"release"`
	Providers        []ActiveProvider `json:"providers"`
}

type ReleaseIdentity struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
}

type ActiveProvider struct {
	ProviderID        string         `json:"providerId"`
	ProviderVersion   string         `json:"providerVersion"`
	ProviderAPI       int            `json:"providerApiVersion"`
	BundleSHA256      string         `json:"bundleSha256"`
	BundleSizeBytes   int64          `json:"bundleSizeBytes"`
	ManifestSHA256    string         `json:"manifestSha256"`
	ModuleSHA256      string         `json:"moduleSha256"`
	ClientModulePath  string         `json:"clientModulePath"`
	InstallationPath  string         `json:"installationPath"`
	FileCount         int64          `json:"fileCount"`
	UnpackedSizeBytes int64          `json:"unpackedSizeBytes"`
	Targets           []ActiveTarget `json:"targets"`
}

type ActiveTarget struct {
	ID         string      `json:"id"`
	Checkpoint *Checkpoint `json:"checkpoint"`
}

func ParseActiveDescriptor(contents []byte) (ActiveDescriptor, error) {
	if !validActiveRawShape(contents) {
		return ActiveDescriptor{}, ErrActiveInvalid
	}
	var result ActiveDescriptor
	if err := decodeClosed(contents, &result); err != nil || !validActive(result) {
		return ActiveDescriptor{}, ErrActiveInvalid
	}
	return result, nil
}

func validActiveRawShape(contents []byte) bool {
	value, err := parseStrictJSON(contents)
	active, ok := value.(map[string]any)
	if err != nil || !ok ||
		!exactMap(active, "schemaVersion", "source", "sourceTreeSha256", "release", "providers") ||
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
	return ok && exactMap(release, "repository", "tag", "commit")
}

func validActiveRawProvider(value any) bool {
	provider, ok := value.(map[string]any)
	if !ok || !exactMap(provider,
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
	if !ok || !exactMap(target, "id", "checkpoint") {
		return false
	}
	if target["checkpoint"] == nil {
		return true
	}
	checkpoint, ok := target["checkpoint"].(map[string]any)
	return ok && exactMap(checkpoint, "writeFormat", "readFormats", "maxBytes")
}

func validActive(value ActiveDescriptor) bool {
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

func validRelease(value ReleaseIdentity) bool {
	return value.Repository == providerRepository && releaseTagPattern.MatchString(value.Tag) &&
		commitPattern.MatchString(value.Commit)
}

func validActiveProvider(value ActiveProvider) bool {
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

func validActiveProviderIdentity(value ActiveProvider) bool {
	return identityPattern.MatchString(value.ProviderID) && semverPattern.MatchString(value.ProviderVersion) &&
		value.ProviderAPI == 1 && digestPattern(value.BundleSHA256) && digestPattern(value.ManifestSHA256) &&
		digestPattern(value.ModuleSHA256) && value.ClientModulePath == "client.mjs" &&
		value.InstallationPath == value.ProviderID+"/"+value.BundleSHA256
}

func validActiveProviderSize(value ActiveProvider) bool {
	return positiveSafe(value.BundleSizeBytes) && value.FileCount >= 3 && value.FileCount <= 100000 &&
		positiveSafe(value.UnpackedSizeBytes)
}

func validActiveTarget(value ActiveTarget) bool {
	return identityPattern.MatchString(value.ID) &&
		(value.Checkpoint == nil || validCheckpoint(*value.Checkpoint))
}

func positiveSafe(value int64) bool {
	return value > 0 && value <= 9007199254740991
}
