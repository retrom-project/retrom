package runtimeclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func (c *Client) loadDevelopment(directory string) error {
	primary := filepath.Join(directory, "dev/dev-provider.json")
	additional, err := filepath.Glob(filepath.Join(directory, "dev/providers/*/dev-provider.json"))
	if err != nil {
		return fmt.Errorf("list development Providers: %w", err)
	}
	seen := make(map[string]bool)
	for _, path := range append([]string{primary}, additional...) {
		raw, readErr := readDevelopmentDescriptor(path)
		if path == primary && errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return fmt.Errorf("read development Provider: %w", readErr)
		}
		if err = c.loadDevelopmentProvider(filepath.Dir(path), raw, seen); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) loadDevelopmentProvider(directory string, raw []byte, seen map[string]bool) error {
	var descriptor struct {
		SchemaVersion      int               `json:"schemaVersion"`
		ProviderID         string            `json:"providerId"`
		BaseBundleSHA256   string            `json:"baseBundleSha256"`
		TargetFingerprints map[string]string `json:"targetFingerprints"`
		ProviderManifest   json.RawMessage   `json:"providerManifest"`
		Files              []struct {
			Path      string `json:"path"`
			SHA256    string `json:"sha256"`
			SizeBytes int64  `json:"sizeBytes"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return fmt.Errorf("decode development Provider: %w", err)
	}
	provider, exists := c.Providers[descriptor.ProviderID]
	if !exists || seen[descriptor.ProviderID] || descriptor.SchemaVersion != 1 ||
		provider.BundleSHA256 != descriptor.BaseBundleSHA256 {
		return model.ErrInvalid
	}
	seen[descriptor.ProviderID] = true
	filesDirectory := filepath.Join(directory, "files")
	root, err := os.OpenRoot(filesDirectory)
	if err != nil {
		return fmt.Errorf("open development assets: %w", err)
	}
	defer closeRoot(root)
	overrides := make(map[string]DevelopmentAsset, len(descriptor.Files))
	if len(descriptor.Files) > 4096 {
		return model.ErrInvalid
	}
	for _, file := range descriptor.Files {
		if _, duplicate := overrides[file.Path]; duplicate {
			return model.ErrInvalid
		}
		if err := verifyDevelopmentAsset(root, file.Path, file.SHA256, file.SizeBytes); err != nil {
			return err
		}
		overrides[file.Path] = DevelopmentAsset{Root: filesDirectory, Name: file.SHA256}
		c.Assets[provider.ProviderID][file.Path] = true
		if file.Path == "client.mjs" {
			provider.ModuleSHA256 = file.SHA256
		}
	}
	if _, exists = overrides["client.mjs"]; !exists {
		return model.ErrInvalid
	}
	for target, digest := range descriptor.TargetFingerprints {
		if !model.Hash(digest) {
			return model.ErrInvalid
		}
		c.Fingerprints[provider.ProviderID+"/"+target] = digest
	}
	provider.Manifest = descriptor.ProviderManifest
	c.Providers[provider.ProviderID] = provider
	c.Overrides[provider.ProviderID] = overrides
	return nil
}

// Development files are immutable, digest-addressed objects. Keep only their
// verified locations in memory; large core binaries are streamed by HTTP.
type DevelopmentAsset struct{ Root, Name string }

func verifyDevelopmentAsset(root *os.Root, path, hash string, size int64) error {
	if !storage.SafeRelative(path) || !model.Hash(hash) || size < 1 {
		return model.ErrInvalid
	}
	info, err := root.Lstat(hash)
	if err != nil {
		return fmt.Errorf("stat development asset: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return model.ErrInvalid
	}
	return verifyFile(root, hash, hash, size)
}

func readDevelopmentDescriptor(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open development descriptor: %w", err)
	}
	defer closeFile(file)
	const limit = 64 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read development descriptor: %w", err)
	}
	if len(data) > limit {
		return nil, model.ErrInvalid
	}
	return data, nil
}
