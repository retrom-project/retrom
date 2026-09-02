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
	ID                       string      `json:"id"`
	GameCompatibilityLine    string      `json:"gameCompatibilityLine"`
	NetplayCompatibilityLine *string     `json:"netplayCompatibilityLine"`
	Checkpoint               *Checkpoint `json:"checkpoint"`
	ContractSHA256           string      `json:"targetContractSha256"`
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
	if err != nil || !ok || !exactMap(active, "schemaVersion", "source", "sourceTreeSha256", "release", "providers") {
		return false
	}
	if active["release"] != nil {
		release, ok := active["release"].(map[string]any)
		if !ok || !exactMap(release, "repository", "tag", "commit") {
			return false
		}
	}
	providers, ok := active["providers"].([]any)
	if !ok {
		return false
	}
	for _, providerValue := range providers {
		provider, ok := providerValue.(map[string]any)
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
		for _, targetValue := range targets {
			target, ok := targetValue.(map[string]any)
			if !ok || !exactMap(target, "id", "gameCompatibilityLine", "netplayCompatibilityLine", "checkpoint", "targetContractSha256") {
				return false
			}
			if target["checkpoint"] != nil {
				checkpoint, ok := target["checkpoint"].(map[string]any)
				if !ok || !exactMap(checkpoint, "writeFormat", "readFormats", "maxBytes") {
					return false
				}
			}
		}
	}
	return true
}

func validActive(value ActiveDescriptor) bool {
	if value.SchemaVersion != 1 || len(value.Providers) == 0 {
		return false
	}
	if value.Source == "candidate" {
		if value.Release != nil || value.SourceTreeSHA256 == nil || !digestPattern(*value.SourceTreeSHA256) {
			return false
		}
	} else if value.Source == "production" {
		if value.SourceTreeSHA256 != nil || value.Release == nil || !validRelease(*value.Release) {
			return false
		}
	} else {
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
	return value.Repository == providerRepository && releaseTagPattern.MatchString(value.Tag) && commitPattern.MatchString(value.Commit)
}

func validActiveProvider(value ActiveProvider) bool {
	if !identityPattern.MatchString(value.ProviderID) || !semverPattern.MatchString(value.ProviderVersion) ||
		value.ProviderAPI != 1 || !digestPattern(value.BundleSHA256) || !digestPattern(value.ManifestSHA256) ||
		!digestPattern(value.ModuleSHA256) || value.ClientModulePath != "client.mjs" ||
		value.InstallationPath != value.ProviderID+"/"+value.BundleSHA256 || !positiveSafe(value.BundleSizeBytes) ||
		value.FileCount < 3 || value.FileCount > 100000 || !positiveSafe(value.UnpackedSizeBytes) || len(value.Targets) == 0 {
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

func validActiveTarget(value ActiveTarget) bool {
	if !identityPattern.MatchString(value.ID) || !tokenPattern.MatchString(value.GameCompatibilityLine) ||
		!digestPattern(value.ContractSHA256) || value.Checkpoint != nil && !validCheckpoint(*value.Checkpoint) {
		return false
	}
	return value.NetplayCompatibilityLine == nil || tokenPattern.MatchString(*value.NetplayCompatibilityLine)
}

func positiveSafe(value int64) bool {
	return value > 0 && value <= 9007199254740991
}
