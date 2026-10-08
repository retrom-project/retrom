package runtimeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"retrom/internal/model"
)

type Client struct {
	Root             string
	Node             string
	Catalog          model.Catalog
	Bindings         []Binding
	Providers        map[string]Provider
	Fingerprints     map[string]string
	Assets           map[string]map[string]bool
	Overrides        map[string]map[string][]byte
	BiosRequirements []BiosRequirement
	Locate           func(string) (string, error)
	pool             *workerPool
}
type (
	Binding struct {
		CoreID       string   `json:"coreId"`
		ProviderID   string   `json:"providerId"`
		TargetID     string   `json:"targetId"`
		PlatformIDs  []string `json:"platformIds"`
		ContentKinds []string `json:"contentKinds"`
	}
	Provider struct {
		ProviderID       string          `json:"providerId"`
		ProviderVersion  string          `json:"providerVersion"`
		BundleSHA256     string          `json:"bundleSha256"`
		ModuleSHA256     string          `json:"moduleSha256"`
		InstallationPath string          `json:"installationPath"`
		Root             string          `json:"-"`
		Manifest         json.RawMessage `json:"-"`
	}
	File struct {
		LogicalKey string `json:"logicalKey"`
		Name       string `json:"name"`
		SHA256     string `json:"sha256"`
		SizeBytes  int64  `json:"sizeBytes"`
	}
	Checkpoint struct {
		WriteFormat string   `json:"writeFormat"`
		ReadFormats []string `json:"readFormats"`
		MaxBytes    int64    `json:"maxBytes"`
		Semantics   string   `json:"semantics"`
	}
	Resource struct {
		Role            string            `json:"role"`
		Kind            string            `json:"kind"`
		Ordinal         int               `json:"ordinal"`
		Files           []string          `json:"files"`
		EntryFile       string            `json:"entryFile,omitempty"`
		BridgeAssetPath string            `json:"bridgeAssetPath,omitempty"`
		Encoding        string            `json:"encoding,omitempty"`
		Paths           map[string]string `json:"paths,omitempty"`
	}
	Prepared struct {
		CoreID           string            `json:"coreId"`
		ProviderID       string            `json:"providerId"`
		TargetID         string            `json:"targetId"`
		CoreFingerprint  string            `json:"coreFingerprint"`
		ROMHash          string            `json:"romHash"`
		Config           json.RawMessage   `json:"config"`
		TargetOptions    json.RawMessage   `json:"targetOptions"`
		Checkpoint       *Checkpoint       `json:"checkpoint"`
		BiosRequirements []BiosRequirement `json:"biosRequirements"`
		Dependencies     json.RawMessage   `json:"dependencies"`
		Capabilities     json.RawMessage   `json:"capabilities"`
		Resources        []Resource        `json:"resources"`
	}
)

func (c *Client) Call(ctx context.Context, command string, input, output any) error {
	return c.pool.Call(ctx, command, input, output)
}

func Files(files []model.GameFile) []File {
	result := make([]File, 0, len(files))
	for _, file := range files {
		result = append(result,
			File{
				LogicalKey: file.LogicalKey,
				Name:       file.LogicalKey,
				SHA256:     file.SHA256,
				SizeBytes:  file.SizeBytes,
			})
	}
	return result
}

func (c *Client) Prepare(ctx context.Context,
	directory model.Directory,
	config json.RawMessage,
	files []model.GameFile,
	core string,
	saved *model.Extinfo) (Prepared,
	error,
) {
	var result Prepared
	input := map[string]any{
		"directory": map[string]any{
			"platformId":     directory.PlatformID,
			"defaultCoreId":  directory.DefaultCoreID,
			"allowedCoreIds": directory.CoreIDs,
		},
		"config":       config,
		"files":        Files(files),
		"fingerprints": c.Fingerprints,
	}
	locators := make(map[string]string, len(files))
	if c.Locate != nil {
		for _, file := range files {
			absolute, locateErr := c.Locate(file.StorageKey)
			if locateErr != nil {
				return result, fmt.Errorf("runtime file locator: %w", locateErr)
			}
			locators[file.LogicalKey] = absolute
		}
	}
	input["locators"] = locators
	if core != "" {
		input["coreId"] = core
	}
	if saved != nil {
		input["savedContext"] = saved
	}
	err := c.Call(ctx, "prepare", input, &result)
	if err == nil && result.Checkpoint != nil && result.Checkpoint.Semantics != "INSTANT" &&
		result.Checkpoint.Semantics != "GAME_SAVE" {
		return result, fmt.Errorf("runtime checkpoint semantics: %w", model.ErrInvalid)
	}
	return result, err
}

func (c *Client) Validate(ctx context.Context, platform string, config json.RawMessage, files []model.GameFile) error {
	cores := make([]string, 0)
	for _, binding := range c.Bindings {
		for _, id := range binding.PlatformIDs {
			if id == platform {
				cores = append(cores, binding.CoreID)
			}
		}
	}
	if len(cores) == 0 {
		return model.ErrInvalid
	}
	_, err := c.Identity(ctx, model.Directory{PlatformID: platform, CoreIDs: cores, DefaultCoreID: cores[0]},
		config, files, "")
	return err
}
func (c *Client) GetCatalog(_ context.Context) (model.Catalog, error) { return c.Catalog, nil }

func Open(ctx context.Context, root, node, providerRoot string) (*Client, error) {
	for _, name := range []string{"scripts/runtime-cli.mjs", "dist/runtime/index.js"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			return nil, fmt.Errorf("runtime tool root: %w", err)
		}
	}
	c := &Client{
		Root:         root,
		Node:         node,
		Providers:    make(map[string]Provider),
		Fingerprints: make(map[string]string),
		Assets:       make(map[string]map[string]bool),
		Overrides:    make(map[string]map[string][]byte),
		pool:         newWorkerPool(ctx, root, node),
	}
	ready := false
	defer func() {
		if !ready {
			c.Close()
		}
	}()
	if err := c.loadProviders(providerRoot); err != nil {
		return nil, err
	}
	if err := c.loadDevelopment(providerRoot); err != nil {
		return nil, err
	}
	var catalog struct {
		Cores []struct {
			ID          string   `json:"id"`
			DisplayName string   `json:"displayName"`
			PlatformIDs []string `json:"platformIds"`
		} `json:"cores"`
		Providers        []json.RawMessage `json:"providers"`
		Bindings         []Binding         `json:"bindings"`
		BiosRequirements []BiosRequirement `json:"biosRequirements"`
		Platforms        []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"platforms"`
	}
	if err := c.Call(ctx, "catalog", nil, &catalog); err != nil {
		return nil, err
	}
	c.BiosRequirements = catalog.BiosRequirements
	c.Bindings = catalog.Bindings
	c.Catalog.Providers = catalog.Providers
	c.Catalog.Bindings = catalog.Bindings
	c.projectCatalog()
	coreNames := make(map[string]string)
	for _, core := range catalog.Cores {
		coreNames[core.ID] = core.DisplayName
	}
	for i := range c.Catalog.Cores {
		if name := coreNames[c.Catalog.Cores[i].ID]; name != "" {
			c.Catalog.Cores[i].Name = name
		}
	}
	names := make(map[string]string)
	for _, platform := range catalog.Platforms {
		names[platform.ID] = platform.DisplayName
	}
	for i := range c.Catalog.Platforms {
		if name := names[c.Catalog.Platforms[i].ID]; name != "" {
			c.Catalog.Platforms[i].Name = name
		}
	}
	ready = true
	return c, nil
}

func (c *Client) AssetAllowed(provider, name string) bool { return c.Assets[provider][name] }

type Identity struct {
	CoreID          string   `json:"coreId"`
	ProviderID      string   `json:"providerId"`
	TargetID        string   `json:"targetId"`
	CoreFingerprint string   `json:"coreFingerprint"`
	ROMHash         string   `json:"romHash"`
	ReadFormats     []string `json:"readFormats"`
	Error           string   `json:"error"`
}

func (c *Client) Identity(ctx context.Context, directory model.Directory, config json.RawMessage,
	files []model.GameFile, core string,
) (Identity, error) {
	input := map[string]any{
		"directory": map[string]any{
			"platformId":    directory.PlatformID,
			"defaultCoreId": directory.DefaultCoreID, "allowedCoreIds": directory.CoreIDs,
		}, "config": config,
		"files": Files(files), "fingerprints": c.Fingerprints,
	}
	if core != "" {
		input["coreId"] = core
	}
	var result []Identity
	if err := c.Call(ctx, "batch-identity", map[string]any{"items": []any{input}}, &result); err != nil {
		return Identity{}, err
	}
	if len(result) != 1 || result[0].Error != "" {
		return Identity{}, model.ErrInvalid
	}
	return result[0], nil
}
