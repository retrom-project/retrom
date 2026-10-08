package runtimeclient

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func (c *Client) loadDevelopment(directory string) error {
	raw, err := os.ReadFile(filepath.Join(directory, "dev/dev-provider.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read development Provider: %w", err)
	}
	var descriptor struct {
		SchemaVersion      int               `json:"schemaVersion"`
		ProviderID         string            `json:"providerId"`
		BaseBundleSHA256   string            `json:"baseBundleSha256"`
		TargetFingerprints map[string]string `json:"targetFingerprints"`
		ProviderManifest   json.RawMessage   `json:"providerManifest"`
		Files              []struct {
			Path          string `json:"path"`
			SHA256        string `json:"sha256"`
			SizeBytes     int64  `json:"sizeBytes"`
			ContentBase64 string `json:"contentBase64"`
		} `json:"files"`
	}
	if err = json.Unmarshal(raw, &descriptor); err != nil {
		return fmt.Errorf("decode development Provider: %w", err)
	}
	provider, exists := c.Providers[descriptor.ProviderID]
	if !exists || descriptor.SchemaVersion != 1 || provider.BundleSHA256 != descriptor.BaseBundleSHA256 {
		return model.ErrInvalid
	}
	overrides := make(map[string][]byte, len(descriptor.Files))
	var total int
	for _, file := range descriptor.Files {
		data, decodeErr := developmentAsset(file.Path, file.ContentBase64, file.SHA256, file.SizeBytes)
		if decodeErr != nil {
			return decodeErr
		}
		total += len(data)
		if total > 64*1024*1024 {
			return model.ErrInvalid
		}
		overrides[file.Path] = data
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

func developmentAsset(path, content, hash string, size int64) ([]byte, error) {
	if !storage.SafeRelative(path) || !model.Hash(hash) {
		return nil, model.ErrInvalid
	}
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("decode development asset: %w", err)
	}
	digest := sha256.Sum256(data)
	if int64(len(data)) != size || hex.EncodeToString(digest[:]) != hash {
		return nil, model.ErrInvalid
	}
	return data, nil
}
