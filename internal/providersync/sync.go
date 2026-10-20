package providersync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Mode selects the version rules and cleanup scope.
type Mode string

const (
	// ModeProduction forbids downgrades and same-version rebuilds and removes unreferenced data.
	ModeProduction Mode = "production"
	// ModeTooling accepts any pinned version and keeps a shared chunk cache intact.
	ModeTooling Mode = "tooling"
)

// spaceMargin keeps headroom for metadata, staging and other writers on the same filesystem.
const spaceMargin = 64 << 20

// Options configures one synchronization of a Provider root.
type Options struct {
	Inputs string
	Root   string
	// Cache holds chunk directories; it defaults to <Root>/chunks.
	Cache  string
	Source string
	// Seeds are local directories of already verified chunks (<seed>/<chunkId>/), linked
	// before downloading; a shared host cache lets new roots install without the network.
	Seeds []string
	Mode  Mode
	// Full rehashes every installed file instead of the metadata quick check.
	Full   bool
	Logger *slog.Logger
	Client *http.Client
	Now    func() time.Time
}

// Result reports what one synchronization did.
type Result struct {
	Reused            bool
	DownloadedChunks  int
	DownloadedBytes   int64
	SeededChunks      int
	RepairedChunks    int
	RemovedChunks     int
	RemovedBundles    int
	ProviderVersions  map[string]string
	CheckedFiles      int
	QuickCheckElapsed time.Duration
}

type syncer struct {
	options Options
	inputs  *Inputs
	root    string
	cache   string
	source  Source
	logger  *slog.Logger
	result  Result
}

type readyProvider struct {
	ProviderID       string   `json:"providerId"`
	ProviderVersion  string   `json:"providerVersion"`
	BundleSHA256     string   `json:"bundleSha256"`
	InstallationPath string   `json:"installationPath"`
	Chunks           []string `json:"chunks"`
}

type readyMarker struct {
	SchemaVersion int             `json:"schemaVersion"`
	InputsSHA256  string          `json:"inputsSha256"`
	Providers     []readyProvider `json:"providers"`
	CompletedAtMs int64           `json:"completedAtMs"`
}

// Sync makes Root hold exactly the pinned Providers. When the completion marker
// matches the pin, only a metadata check runs and nothing is downloaded.
func Sync(ctx context.Context, options Options) (Result, error) {
	s, err := newSyncer(options)
	if err != nil {
		return Result{}, err
	}
	release, err := lock(s.root)
	if err != nil {
		return Result{}, err
	}
	defer release()
	if s.cache != filepath.Join(s.root, "chunks") {
		releaseCache, cacheErr := lock(s.cache)
		if cacheErr != nil {
			return Result{}, cacheErr
		}
		defer releaseCache()
	}
	if err = s.run(ctx); err != nil {
		return s.result, err
	}
	return s.result, nil
}

func newSyncer(options Options) (*syncer, error) {
	if options.Mode != ModeProduction && options.Mode != ModeTooling {
		return nil, ErrInputsInvalid
	}
	inputs, err := LoadInputs(options.Inputs)
	if err != nil {
		return nil, err
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve Provider root: %w", err)
	}
	cache := filepath.Join(root, "chunks")
	if options.Cache != "" {
		if cache, err = filepath.Abs(options.Cache); err != nil {
			return nil, fmt.Errorf("resolve chunk cache: %w", err)
		}
	}
	versions := make(map[string]string, len(inputs.Providers))
	for _, provider := range inputs.Providers {
		versions[provider.ProviderID] = provider.ProviderVersion
	}
	return &syncer{
		options: options, inputs: inputs, root: root, cache: cache, logger: options.Logger,
		result: Result{ProviderVersions: versions},
	}, nil
}

func (s *syncer) run(ctx context.Context) error {
	if err := s.checkVersions(); err != nil {
		return err
	}
	marker, manifests := s.readyState()
	if marker != nil {
		bad := s.check(manifests)
		if bad.empty() {
			s.result.Reused = true
			return s.cleanup(marker)
		}
		if err := s.discard(bad); err != nil {
			return err
		}
	}
	return s.install(ctx)
}

// transport resolves the download source on first use; a fully seeded sync needs none.
func (s *syncer) transport() (Source, error) {
	if s.source == nil {
		source, err := NewSource(s.options.Source, s.inputs, s.options.Client)
		if err != nil {
			return nil, err
		}
		s.source = source
	}
	return s.source, nil
}

func (s *syncer) install(ctx context.Context) error {
	manifests, err := s.fetchManifests(ctx)
	if err != nil {
		return err
	}
	if err = s.ensureSpace(manifests); err != nil {
		return err
	}
	if err = s.ensureChunks(ctx, manifests); err != nil {
		return err
	}
	entries := make([]activeProvider, 0, len(manifests))
	for index, record := range s.inputs.Providers {
		entry, assembleErr := s.assemble(record, manifests[index])
		if assembleErr != nil {
			return assembleErr
		}
		entries = append(entries, entry)
	}
	if err = s.writeActive(entries); err != nil {
		return err
	}
	marker, err := s.writeReady(manifests)
	if err != nil {
		return err
	}
	return s.cleanup(marker)
}

func (s *syncer) fetchManifests(ctx context.Context) ([]*ChunkManifest, error) {
	manifests := make([]*ChunkManifest, 0, len(s.inputs.Providers))
	directory := filepath.Join(s.root, "manifests")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create manifest directory: %w", err)
	}
	for _, record := range s.inputs.Providers {
		raw, err := s.manifestBytes(ctx, record.ChunkManifest)
		if err != nil {
			return nil, err
		}
		manifest, err := parseManifest(raw, record)
		if err != nil {
			return nil, err
		}
		if err = writeAtomic(filepath.Join(directory, record.ChunkManifest.Asset), raw); err != nil {
			return nil, err
		}
		// A shared cache keeps verified manifests next to its chunks, so it can seed other roots.
		if s.cache != filepath.Join(s.root, "chunks") {
			if err = os.MkdirAll(filepath.Join(s.cache, "manifests"), 0o755); err != nil {
				return nil, fmt.Errorf("create cache manifests: %w", err)
			}
			if err = writeAtomic(filepath.Join(s.cache, "manifests", record.ChunkManifest.Asset), raw); err != nil {
				return nil, err
			}
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

// manifestBytes prefers a local copy (seeds, then the chunk cache); the pinned digest
// makes any location trustworthy, so a warm cache prepares without the network.
func (s *syncer) manifestBytes(ctx context.Context, asset Asset) ([]byte, error) {
	for _, seed := range append(slices.Clone(s.options.Seeds), s.cache) {
		raw, err := os.ReadFile(filepath.Join(seed, "manifests", asset.Asset))
		if err == nil && int64(len(raw)) == asset.SizeBytes && digest(raw) == asset.SHA256 {
			return raw, nil
		}
	}
	source, err := s.transport()
	if err != nil {
		return nil, err
	}
	return fetchBytes(ctx, source, asset)
}

func (s *syncer) ensureSpace(manifests []*ChunkManifest) error {
	var needed int64
	for _, manifest := range manifests {
		for _, chunk := range manifest.Chunks {
			if !exists(filepath.Join(s.cache, chunk.ChunkID)) {
				needed += chunk.UnpackedSizeBytes + chunk.ArchiveSizeBytes
			}
		}
	}
	if err := os.MkdirAll(s.cache, 0o755); err != nil {
		return fmt.Errorf("create chunk cache: %w", err)
	}
	available, err := freeSpace(s.cache)
	if err != nil {
		return err
	}
	if needed > 0 && available < needed+spaceMargin {
		return fmt.Errorf("need %d bytes, %d available: %w", needed+spaceMargin, available, ErrSpace)
	}
	return nil
}

func (s *syncer) ensureChunks(ctx context.Context, manifests []*ChunkManifest) error {
	var total, done int64
	for _, manifest := range manifests {
		for _, chunk := range manifest.Chunks {
			if !exists(filepath.Join(s.cache, chunk.ChunkID)) {
				total += chunk.ArchiveSizeBytes
			}
		}
	}
	for _, manifest := range manifests {
		for _, chunk := range manifest.Chunks {
			downloaded, err := s.ensureChunk(ctx, chunk)
			if err != nil {
				return err
			}
			if downloaded {
				done += chunk.ArchiveSizeBytes
				s.result.DownloadedChunks++
				s.result.DownloadedBytes += chunk.ArchiveSizeBytes
				s.logger.Info("Provider chunk ready", "provider", manifest.ProviderID, "chunk", chunk.ChunkID[:12],
					"downloadedBytes", done, "totalBytes", total)
			}
		}
	}
	return nil
}

func (s *syncer) writeActive(entries []activeProvider) error {
	slices.SortFunc(entries, func(left, right activeProvider) int {
		return strings.Compare(left.ProviderID, right.ProviderID)
	})
	value := map[string]any{
		"schemaVersion": 1, "providers": entries, "release": s.inputs.Release,
		"source": "production", "sourceTreeSha256": nil,
	}
	if s.inputs.Release == nil {
		value["source"], value["sourceTreeSha256"] = "candidate", s.inputs.SourceTreeSHA256
	}
	return writeJSON(filepath.Join(s.root, "active.json"), value)
}

func (s *syncer) writeReady(manifests []*ChunkManifest) (*readyMarker, error) {
	marker := &readyMarker{SchemaVersion: 1, InputsSHA256: s.inputs.Digest, CompletedAtMs: s.options.Now().UnixMilli()}
	for index, record := range s.inputs.Providers {
		chunks := make([]string, 0, len(manifests[index].Chunks))
		for _, chunk := range manifests[index].Chunks {
			chunks = append(chunks, chunk.ChunkID)
		}
		slices.Sort(chunks)
		marker.Providers = append(marker.Providers, readyProvider{
			ProviderID:      record.ProviderID,
			ProviderVersion: record.ProviderVersion, BundleSHA256: record.BundleSHA256,
			InstallationPath: installationPath(record), Chunks: chunks,
		})
	}
	return marker, writeJSON(filepath.Join(s.root, "ready.json"), marker)
}

// readyState returns the marker and stored manifests only when they match the current pin.
func (s *syncer) readyState() (*readyMarker, []*ChunkManifest) {
	var marker readyMarker
	raw, err := os.ReadFile(filepath.Join(s.root, "ready.json"))
	if err != nil || json.Unmarshal(raw, &marker) != nil || marker.SchemaVersion != 1 ||
		marker.InputsSHA256 != s.inputs.Digest {
		return nil, nil
	}
	manifests := make([]*ChunkManifest, 0, len(s.inputs.Providers))
	for _, record := range s.inputs.Providers {
		raw, err = os.ReadFile(filepath.Join(s.root, "manifests", record.ChunkManifest.Asset))
		if err != nil || digest(raw) != record.ChunkManifest.SHA256 {
			return nil, nil
		}
		manifest, parseErr := parseManifest(raw, record)
		if parseErr != nil {
			return nil, nil
		}
		manifests = append(manifests, manifest)
	}
	return &marker, manifests
}

// checkVersions applies the production upgrade rules against the installed active.json.
func (s *syncer) checkVersions() error {
	if s.options.Mode != ModeProduction {
		return nil
	}
	var active struct {
		Providers []activeProvider `json:"providers"`
	}
	raw, err := os.ReadFile(filepath.Join(s.root, "active.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || json.Unmarshal(raw, &active) != nil {
		return fmt.Errorf("read installed Providers: %w", ErrInputsInvalid)
	}
	for _, installed := range active.Providers {
		for _, pinned := range s.inputs.Providers {
			if pinned.ProviderID != installed.ProviderID {
				continue
			}
			order := compareVersions(pinned.ProviderVersion, installed.ProviderVersion)
			if order < 0 {
				return fmt.Errorf("%s %s is older than installed %s: %w", pinned.ProviderID,
					pinned.ProviderVersion, installed.ProviderVersion, ErrDowngrade)
			}
			if order == 0 && pinned.BundleSHA256 != installed.BundleSHA256 {
				return fmt.Errorf("%s %s: %w", pinned.ProviderID, pinned.ProviderVersion, ErrRebuilt)
			}
		}
	}
	return nil
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeAtomic(path, append(raw, '\n'))
}
