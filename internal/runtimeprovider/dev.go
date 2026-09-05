package runtimeprovider

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"retrom/internal/runtimebundle"
)

type devProvider struct {
	providerID string
	bundleSHA  string
	moduleSHA  string
	files      map[string]staticFile
}

type devDescriptor struct {
	SchemaVersion    int                 `json:"schemaVersion"`
	ProviderID       string              `json:"providerId"`
	BaseBundleSHA256 string              `json:"baseBundleSha256"`
	Files            []devFileDescriptor `json:"files"`
}

type devFileDescriptor struct {
	Path          string `json:"path"`
	SizeBytes     int64  `json:"sizeBytes"`
	SHA256        string `json:"sha256"`
	MediaType     string `json:"mediaType"`
	ContentBase64 string `json:"contentBase64"`
}

func loadDevProvider(rawRoot string, active runtimebundle.ActiveDescriptor) (*devProvider, error) {
	root, err := filepath.Abs(rawRoot)
	if err != nil || root != filepath.Clean(rawRoot) {
		return nil, ErrInstallationInvalid
	}
	contents, err := readMetadata(filepath.Join(root, "dev-provider.json"))
	if err != nil {
		return nil, installationInvalid(err)
	}
	descriptor, err := parseDevDescriptor(contents)
	if err != nil {
		return nil, err
	}
	activeProvider := matchingActiveProvider(active, descriptor)
	if activeProvider == nil {
		return nil, ErrInstallationInvalid
	}
	result := &devProvider{
		providerID: descriptor.ProviderID, bundleSHA: descriptor.BaseBundleSHA256,
		files: make(map[string]staticFile, len(descriptor.Files)),
	}
	if err := result.loadFiles(descriptor.Files, activeProvider.ClientModulePath); err != nil {
		return nil, err
	}
	if result.moduleSHA == "" {
		return nil, ErrInstallationInvalid
	}
	return result, nil
}

func parseDevDescriptor(contents []byte) (devDescriptor, error) {
	var descriptor devDescriptor
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&descriptor); err != nil {
		return devDescriptor{}, installationInvalid(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || descriptor.SchemaVersion != 1 ||
		!providerIDPattern.MatchString(descriptor.ProviderID) || !digestPattern.MatchString(descriptor.BaseBundleSHA256) ||
		len(descriptor.Files) == 0 {
		return devDescriptor{}, ErrInstallationInvalid
	}
	return descriptor, nil
}

func matchingActiveProvider(
	active runtimebundle.ActiveDescriptor,
	descriptor devDescriptor,
) *runtimebundle.ActiveProvider {
	for index := range active.Providers {
		provider := &active.Providers[index]
		if provider.ProviderID == descriptor.ProviderID && provider.BundleSHA256 == descriptor.BaseBundleSHA256 {
			return provider
		}
	}
	return nil
}

func (provider *devProvider) loadFiles(files []devFileDescriptor, clientModulePath string) error {
	previous := ""
	for _, file := range files {
		if !safeBundlePath(file.Path) || file.Path <= previous || nonPublicPaths[file.Path] ||
			file.SizeBytes < 1 || !digestPattern.MatchString(file.SHA256) || !publicMediaTypes[file.MediaType] {
			return ErrInstallationInvalid
		}
		previous = file.Path
		contents, err := base64.StdEncoding.Strict().DecodeString(file.ContentBase64)
		if err != nil || int64(len(contents)) != file.SizeBytes || digest(contents) != file.SHA256 {
			return ErrInstallationInvalid
		}
		provider.files[file.Path] = staticFile{
			contents: contents, sizeBytes: file.SizeBytes, sha256: file.SHA256, mediaType: file.MediaType,
		}
		if file.Path == clientModulePath && file.MediaType == "text/javascript; charset=utf-8" {
			provider.moduleSHA = file.SHA256
		}
	}
	return nil
}

func (provider *devProvider) apply(active runtimebundle.ActiveDescriptor) runtimebundle.ActiveDescriptor {
	result := active
	result.Providers = append([]runtimebundle.ActiveProvider(nil), active.Providers...)
	for index := range result.Providers {
		if result.Providers[index].ProviderID == provider.providerID {
			result.Providers[index].ModuleSHA256 = provider.moduleSHA
		}
	}
	return result
}
