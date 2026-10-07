package runtimeclient

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func (c *Client) loadProviders(directory string) error {
	data, err := os.ReadFile(filepath.Join(directory, "active.json"))
	if err != nil {
		return fmt.Errorf("read active Providers: %w", err)
	}
	var active struct {
		Providers []Provider `json:"providers"`
	}
	if err = json.Unmarshal(data, &active); err != nil {
		return fmt.Errorf("decode active Providers: %w", err)
	}
	for _, provider := range active.Providers {
		if err = c.loadProvider(directory, provider); err != nil {
			return err
		}
	}

	if len(c.Providers) == 0 {
		return model.ErrUnavailable
	}
	return nil
}

func (c *Client) loadProvider(directory string, provider Provider) error {
	if !storage.SafeRelative(provider.InstallationPath) || !model.Hash(provider.BundleSHA256) {
		return model.ErrInvalid
	}
	if !model.Hash(provider.ModuleSHA256) {
		return model.ErrInvalid
	}
	provider.Root = filepath.Join(directory, "installed", provider.InstallationPath)
	if err := verifyProvider(provider.Root); err != nil {
		return err
	}
	rawIntegrity, readErr := os.ReadFile(filepath.Join(provider.Root, "integrity.json"))
	if readErr != nil {
		return fmt.Errorf("read verified integrity: %w", readErr)
	}
	var entries struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rawIntegrity, &entries); err != nil {
		return fmt.Errorf("decode verified integrity: %w", err)
	}
	c.Assets[provider.ProviderID] = make(map[string]bool, len(entries.Files))
	for _, entry := range entries.Files {
		c.Assets[provider.ProviderID][entry.Path] = true
	}
	manifest, readErr := os.ReadFile(filepath.Join(provider.Root, "provider.json"))
	if readErr != nil {
		return fmt.Errorf("read Provider manifest: %w", readErr)
	}
	provider.Manifest = manifest
	var fingerprint struct {
		Targets map[string]struct {
			Fingerprint string `json:"fingerprint"`
		} `json:"targets"`
	}
	raw, readErr := os.ReadFile(filepath.Join(provider.Root, "runtime-fingerprints.json"))
	if readErr != nil {
		return fmt.Errorf("read target fingerprints: %w", readErr)
	}
	if err := json.Unmarshal(raw, &fingerprint); err != nil {
		return fmt.Errorf("decode target fingerprints: %w", err)
	}
	for id, value := range fingerprint.Targets {
		if !model.Hash(value.Fingerprint) {
			return model.ErrInvalid
		}
		c.Fingerprints[provider.ProviderID+"/"+id] = value.Fingerprint
	}
	c.Providers[provider.ProviderID] = provider
	return nil
}

func verifyProvider(directory string) error {
	raw, err := os.ReadFile(filepath.Join(directory, "integrity.json"))
	if err != nil {
		return fmt.Errorf("read Provider integrity: %w", err)
	}
	var integrity struct {
		Files []struct {
			Path      string `json:"path"`
			SHA256    string `json:"sha256"`
			SizeBytes int64  `json:"sizeBytes"`
		} `json:"files"`
	}
	if err = json.Unmarshal(raw, &integrity); err != nil {
		return fmt.Errorf("decode Provider integrity: %w", err)
	}
	if len(integrity.Files) == 0 {
		return model.ErrInvalid
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open Provider root: %w", err)
	}
	defer closeRoot(root)
	for _, entry := range integrity.Files {
		if !storage.SafeRelative(entry.Path) || !model.Hash(entry.SHA256) {
			return model.ErrInvalid
		}
		if err = verifyFile(root, entry.Path, entry.SHA256, entry.SizeBytes); err != nil {
			return err
		}
	}
	return nil
}

func verifyFile(root *os.Root, name, digest string, size int64) error {
	file, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("open Provider asset: %w", err)
	}
	defer closeFile(file)
	hash := sha256.New()
	copied, err := io.Copy(hash, file)
	if err != nil {
		return fmt.Errorf("hash Provider asset: %w", err)
	}
	if copied != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return fmt.Errorf("Provider asset integrity: %w", model.ErrInvalid)
	}
	return nil
}

func (c *Client) projectCatalog() {
	names := c.targetNames()
	platforms := make(map[string]bool)
	c.Catalog.Cores = make([]model.Core, 0, len(c.Bindings))
	positions := make(map[string]int, len(c.Bindings))
	for _, binding := range c.Bindings {
		name := names[binding.ProviderID+"/"+binding.TargetID]
		if name == "" {
			name = binding.CoreID
		}
		digest := c.Fingerprints[binding.ProviderID+"/"+binding.TargetID]
		core := model.Core{ID: binding.CoreID, Name: name, PlatformIDs: binding.PlatformIDs, Fingerprint: &digest}
		if index, exists := positions[core.ID]; exists {
			existing := &c.Catalog.Cores[index]
			existing.PlatformIDs = uniqueStrings(existing.PlatformIDs, core.PlatformIDs)
			if existing.Fingerprint != nil && *existing.Fingerprint != digest {
				existing.Fingerprint = nil
			}
		} else {
			positions[core.ID] = len(c.Catalog.Cores)
			c.Catalog.Cores = append(c.Catalog.Cores, core)
		}

		for _, id := range binding.PlatformIDs {
			platforms[id] = true
		}
	}
	c.Catalog.Platforms = make([]model.Platform, 0, len(platforms))
	for id := range platforms {
		c.Catalog.Platforms = append(c.Catalog.Platforms, model.Platform{ID: id, Name: id})
	}
	sort.Slice(c.Catalog.Platforms, func(i, j int) bool { return c.Catalog.Platforms[i].ID < c.Catalog.Platforms[j].ID })
}

func uniqueStrings(existing, values []string) []string {
	result := append([]string{}, existing...)
	seen := make(map[string]bool, len(result))
	for _, value := range result {
		seen[value] = true
	}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func (c *Client) targetNames() map[string]string {
	names := make(map[string]string)
	for _, raw := range c.Catalog.Providers {
		var manifest struct {
			ProviderID string `json:"providerId"`
			Targets    []struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"targets"`
		}
		if json.Unmarshal(raw, &manifest) != nil {
			continue
		}
		for _, target := range manifest.Targets {
			names[manifest.ProviderID+"/"+target.ID] = target.DisplayName
		}
	}
	return names
}
