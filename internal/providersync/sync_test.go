package providersync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

// fixtureRelease writes a runtime release (bundle files, chunks, manifests and
// runtime-inputs.json) the way the runtime publishes it, one chunk per core.
type fixtureRelease struct {
	dir    string
	inputs string
}

func writeRelease(t *testing.T, version string, cores map[string]string) fixtureRelease {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		"client.mjs":    []byte("export default {};\n"),
		"provider.json": []byte(`{"providerApiVersion":1,"clientModulePath":"client.mjs","targets":[{"id":"core","checkpoint":null}]}`),
	}
	for core, contents := range cores {
		files["assets/"+core+"/core.wasm"] = []byte(contents)
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	entries := make([]integrityFile, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, integrityFile{
			Path: path, SizeBytes: int64(len(files[path])), SHA256: digest(files[path]),
			MediaType: "application/octet-stream",
		})
	}
	integrity, err := json.Marshal(map[string]any{"schemaVersion": 1, "files": entries})
	if err != nil {
		t.Fatal(err)
	}
	files["integrity.json"] = integrity
	groups := map[string][]string{}
	for path := range files {
		group := ""
		if strings.HasPrefix(path, "assets/") {
			group = strings.Join(strings.Split(path, "/")[:2], "/")
		}
		groups[group] = append(groups[group], path)
	}
	manifest := ChunkManifest{SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: version, BundleSHA256: digest(integrity)}
	var unpacked int64
	for _, members := range groups {
		chunk := writeChunk(t, dir, files, members)
		unpacked += chunk.UnpackedSizeBytes
		manifest.Chunks = append(manifest.Chunks, chunk)
	}
	sort.Slice(manifest.Chunks, func(i, j int) bool { return manifest.Chunks[i].ChunkID < manifest.Chunks[j].ChunkID })
	manifestRaw, _ := json.Marshal(manifest)
	manifestName := "fixture-chunks-" + version + ".json"
	writeFile(t, filepath.Join(dir, manifestName), manifestRaw)
	inputs := map[string]any{
		"schemaVersion": 1, "repository": "https://github.com/retrom-project/retrom-runtime",
		"sourceTreeSha256": strings.Repeat("a", 64), "release": nil, "tool": map[string]any{},
		"providers": []ProviderRecord{{
			BundleSHA256: digest(integrity), ChunkManifest: Asset{
				Asset: manifestName, SHA256: digest(manifestRaw),
				SizeBytes: int64(len(manifestRaw)),
			}, FileCount: int64(len(files)), ManifestSHA256: digest(files["provider.json"]),
			ModuleSHA256: digest(files["client.mjs"]), ProviderID: "fixture", ProviderVersion: version, UnpackedSizeBytes: unpacked,
		}},
	}
	inputsRaw, _ := json.Marshal(inputs)
	path := filepath.Join(t.TempDir(), "runtime-inputs.json")
	writeFile(t, path, inputsRaw)
	return fixtureRelease{dir: dir, inputs: path}
}

func writeChunk(t *testing.T, dir string, files map[string][]byte, members []string) Chunk {
	t.Helper()
	sort.Strings(members)
	var tarred bytes.Buffer
	gz := gzip.NewWriter(&tarred)
	writer := tar.NewWriter(gz)
	chunk := Chunk{}
	for _, path := range members {
		contents := files[path]
		if err := writer.WriteHeader(&tar.Header{
			Name: path, Mode: 0o644, Size: int64(len(contents)),
			Typeflag: tar.TypeReg, Format: tar.FormatUSTAR,
		}); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write(contents)
		chunk.Files = append(chunk.Files, ChunkFile{Path: path, SHA256: digest(contents), SizeBytes: int64(len(contents))})
		chunk.UnpackedSizeBytes += int64(len(contents))
	}
	_ = writer.Close()
	_ = gz.Close()
	chunk.ChunkID = chunkID(chunk.Files)
	chunk.Archive = "fixture-chunk-" + chunk.ChunkID + ".tar.gz"
	chunk.ArchiveSHA256, chunk.ArchiveSizeBytes = digest(tarred.Bytes()), int64(tarred.Len())
	writeFile(t, filepath.Join(dir, chunk.Archive), tarred.Bytes())
	return chunk
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}

type countingServer struct {
	*httptest.Server
	chunks atomic.Int64
	fail   atomic.Int64
}

func serve(t *testing.T, dir string) *countingServer {
	t.Helper()
	server := &countingServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if strings.Contains(name, "-chunk-") {
			if server.fail.Load() > 0 && server.chunks.Load() >= server.fail.Load() {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			server.chunks.Add(1)
		}
		http.ServeFile(w, r, filepath.Join(dir, name))
	}))
	t.Cleanup(server.Close)
	return server
}

func sync(t *testing.T, release fixtureRelease, root string, server *countingServer, mode Mode, full bool) (Result, error) {
	t.Helper()
	return Sync(context.Background(), Options{Inputs: release.inputs, Root: root, Source: server.URL, Mode: mode, Full: full})
}

func installedFile(root, version, core string) string {
	_ = version
	matches, _ := filepath.Glob(filepath.Join(root, "installed", "fixture", "*", "assets", core, "core.wasm"))
	if len(matches) != 1 {
		return ""
	}
	return matches[0]
}

func TestFirstSyncInstallsByHardLinkAndWritesMarkerLast(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-1", "beta": "beta-1"})
	server, root := serve(t, release.dir), t.TempDir()
	result, err := sync(t, release, root, server, ModeProduction, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reused || result.DownloadedChunks != 3 || server.chunks.Load() != 3 {
		t.Fatalf("unexpected first sync %+v requests=%d", result, server.chunks.Load())
	}
	var active struct {
		Providers []activeProvider `json:"providers"`
		Source    string           `json:"source"`
	}
	raw, _ := os.ReadFile(filepath.Join(root, "active.json"))
	if json.Unmarshal(raw, &active) != nil || len(active.Providers) != 1 || active.Source != "candidate" ||
		active.Providers[0].InstallationPath != "fixture/"+active.Providers[0].BundleSHA256 {
		t.Fatalf("unexpected active.json %s", raw)
	}
	installed, _ := os.Lstat(installedFile(root, "1.0.0", "alpha"))
	cached, _ := filepath.Glob(filepath.Join(root, "chunks", "*", "assets", "alpha", "core.wasm"))
	cachedInfo, _ := os.Lstat(cached[0])
	if !os.SameFile(installed, cachedInfo) {
		t.Fatal("bundle file is not a hard link of the verified chunk")
	}
	if !exists(filepath.Join(root, "ready.json")) || exists(filepath.Join(root, ".staging")) {
		t.Fatal("marker missing or staging left behind")
	}
}

func TestRestartWithValidMarkerDownloadsNothing(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-1"})
	server, root := serve(t, release.dir), t.TempDir()
	if _, err := sync(t, release, root, server, ModeProduction, false); err != nil {
		t.Fatal(err)
	}
	before := server.chunks.Load()
	result, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: root, Source: "http://127.0.0.1:1",
		Mode: ModeProduction,
	})
	if err != nil || !result.Reused || result.DownloadedChunks != 0 || server.chunks.Load() != before {
		t.Fatalf("restart touched the source: %+v %v", result, err)
	}
}

func TestUpgradeDownloadsOnlyChangedChunksAndRemovesTheOldCore(t *testing.T) {
	first := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-1", "beta": "beta-1"})
	second := writeRelease(t, "1.0.1", map[string]string{"alpha": "alpha-1", "beta": "beta-2"})
	root := t.TempDir()
	if _, err := sync(t, first, root, serve(t, first.dir), ModeProduction, false); err != nil {
		t.Fatal(err)
	}
	alphaBefore, _ := os.Lstat(installedFile(root, "1.0.0", "alpha"))
	server := serve(t, second.dir)
	result, err := sync(t, second, root, server, ModeProduction, false)
	if err != nil {
		t.Fatal(err)
	}
	// beta and the common chunk (integrity.json changes) are new; alpha is reused.
	if result.DownloadedChunks != 2 || server.chunks.Load() != 2 || result.RemovedChunks != 2 || result.RemovedBundles != 1 {
		t.Fatalf("unexpected upgrade %+v", result)
	}
	alphaAfter, _ := os.Lstat(installedFile(root, "1.0.1", "alpha"))
	if !os.SameFile(alphaBefore, alphaAfter) {
		t.Fatal("unchanged core was not reused")
	}
	chunks, _ := os.ReadDir(filepath.Join(root, "chunks"))
	if names := dirNames(chunks); len(names) != 3 {
		t.Fatalf("old chunks were not removed: %v", names)
	}
}

func TestInterruptedSyncResumesWithoutRedownloading(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "a", "beta": "b", "gamma": "c"})
	root, server := t.TempDir(), serve(t, release.dir)
	server.fail.Store(2)
	if _, err := sync(t, release, root, server, ModeProduction, false); err == nil {
		t.Fatal("expected the interrupted sync to fail")
	}
	if exists(filepath.Join(root, "ready.json")) || exists(filepath.Join(root, "active.json")) {
		t.Fatal("an incomplete sync published state")
	}
	server.fail.Store(0)
	result, err := sync(t, release, root, server, ModeProduction, false)
	if err != nil || result.DownloadedChunks != 2 || server.chunks.Load() != 4 {
		t.Fatalf("resume redownloaded completed chunks: %+v requests=%d %v", result, server.chunks.Load(), err)
	}
}

func TestProductionRefusesDowngradeAndRebuildButToolingAccepts(t *testing.T) {
	newer := writeRelease(t, "1.1.0", map[string]string{"alpha": "new"})
	older := writeRelease(t, "1.0.0", map[string]string{"alpha": "old"})
	rebuilt := writeRelease(t, "1.1.0", map[string]string{"alpha": "rebuilt"})
	root := t.TempDir()
	if _, err := sync(t, newer, root, serve(t, newer.dir), ModeProduction, false); err != nil {
		t.Fatal(err)
	}
	active, _ := os.ReadFile(filepath.Join(root, "active.json"))
	if _, err := sync(t, older, root, serve(t, older.dir), ModeProduction, false); !errors.Is(err, ErrDowngrade) {
		t.Fatalf("expected downgrade refusal, got %v", err)
	}
	if _, err := sync(t, rebuilt, root, serve(t, rebuilt.dir), ModeProduction, false); !errors.Is(err, ErrRebuilt) {
		t.Fatalf("expected rebuild refusal, got %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "active.json")); !bytes.Equal(active, after) {
		t.Fatal("a refused image modified the installed Providers")
	}
	if _, err := sync(t, older, root, serve(t, older.dir), ModeTooling, false); err != nil {
		t.Fatalf("tooling must accept any pinned version: %v", err)
	}
}

func TestQuickCheckRepairsOnlyTheDamagedChunk(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes", "beta": "beta-bytes"})
	root, server := t.TempDir(), serve(t, release.dir)
	if _, err := sync(t, release, root, server, ModeProduction, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(installedFile(root, "1.0.0", "beta"), 2); err != nil {
		t.Fatal(err)
	}
	result, err := sync(t, release, root, server, ModeProduction, false)
	if err != nil || result.Reused || result.RepairedChunks != 1 || result.DownloadedChunks != 1 {
		t.Fatalf("unexpected repair %+v %v", result, err)
	}
	if contents, _ := os.ReadFile(installedFile(root, "1.0.0", "beta")); string(contents) != "beta-bytes" {
		t.Fatalf("repair did not restore the file: %q", contents)
	}
}

func TestFullCheckFindsSameSizeTamperingThatTheQuickCheckCannot(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes"})
	root, server := t.TempDir(), serve(t, release.dir)
	if _, err := sync(t, release, root, server, ModeProduction, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, installedFile(root, "1.0.0", "alpha"), []byte("ALPHA-BYTES"))
	if result, err := sync(t, release, root, server, ModeProduction, false); err != nil || !result.Reused {
		t.Fatalf("quick check should only read metadata: %+v %v", result, err)
	}
	result, err := sync(t, release, root, server, ModeProduction, true)
	if err != nil || result.RepairedChunks != 1 {
		t.Fatalf("full check did not repair tampering: %+v %v", result, err)
	}
	if contents, _ := os.ReadFile(installedFile(root, "1.0.0", "alpha")); string(contents) != "alpha-bytes" {
		t.Fatalf("full repair did not restore the file: %q", contents)
	}
}

func TestTamperedChunkArchiveIsRejectedBeforePublication(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes"})
	archives, _ := filepath.Glob(filepath.Join(release.dir, "fixture-chunk-*.tar.gz"))
	for _, archive := range archives {
		raw, _ := os.ReadFile(archive)
		writeFile(t, archive, append(raw, 0))
	}
	root := t.TempDir()
	if _, err := sync(t, release, root, serve(t, release.dir), ModeProduction, false); !errors.Is(err, ErrChunkInvalid) {
		t.Fatalf("expected a chunk verification failure, got %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "chunks")); len(dirNames(entries)) != 0 {
		t.Fatal("an unverified chunk was published")
	}
}

func TestInsufficientSpaceFailsBeforeDownloading(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes"})
	root, server := t.TempDir(), serve(t, release.dir)
	original := freeSpace
	freeSpace = func(string) (int64, error) { return spaceMargin, nil }
	t.Cleanup(func() { freeSpace = original })
	if _, err := sync(t, release, root, server, ModeProduction, false); !errors.Is(err, ErrSpace) {
		t.Fatalf("expected a space failure, got %v", err)
	}
	if server.chunks.Load() != 0 || exists(filepath.Join(root, "active.json")) {
		t.Fatal("downloaded or published despite insufficient space")
	}
}

func TestChunkIdentityMatchesTheRuntimeImplementation(t *testing.T) {
	// The runtime computes sha256(JSON.stringify([[path, size, sha256], ...])).
	files := []ChunkFile{
		{Path: "assets/core/a.js", SizeBytes: 1, SHA256: strings.Repeat("a", 64)},
		{Path: "assets/core/b.wasm", SizeBytes: 2, SHA256: strings.Repeat("b", 64)},
	}
	expected := digest([]byte(`[["assets/core/a.js",1,"` + strings.Repeat("a", 64) + `"],["assets/core/b.wasm",2,"` +
		strings.Repeat("b", 64) + `"]]`))
	if chunkID(files) != expected {
		t.Fatalf("chunk identity diverges from the runtime: %s", chunkID(files))
	}
}

func TestCompareVersions(t *testing.T) {
	ordered := []string{"0.9.0", "0.72.2", "0.72.3-rc.1", "0.72.3-rc.2", "0.72.3", "0.73.0", "1.0.0"}
	for index := 1; index < len(ordered); index++ {
		if compareVersions(ordered[index-1], ordered[index]) >= 0 || compareVersions(ordered[index], ordered[index-1]) <= 0 {
			t.Fatalf("%s must precede %s", ordered[index-1], ordered[index])
		}
	}
	if compareVersions("0.72.3", "0.72.3") != 0 {
		t.Fatal("equal versions must compare equal")
	}
}

func dirNames(entries []os.DirEntry) []string {
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() && lowerDigest.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestSeedInstallsWithoutDownloadingAndContainerStartReusesTheMarker(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes", "beta": "beta-bytes"})
	host, server := t.TempDir(), serve(t, release.dir)
	if _, err := sync(t, release, host, server, ModeTooling, false); err != nil {
		t.Fatal(err)
	}
	volume := t.TempDir()
	seeded, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: volume, Mode: ModeTooling,
		Seeds: []string{host}, Source: "http://127.0.0.1:1",
	})
	if err != nil || seeded.DownloadedChunks != 0 || seeded.SeededChunks != 3 {
		t.Fatalf("seeding downloaded: %+v %v", seeded, err)
	}
	started, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: volume, Mode: ModeProduction,
		Source: "http://127.0.0.1:1",
	})
	if err != nil || !started.Reused {
		t.Fatalf("a seeded volume must start without the network: %+v %v", started, err)
	}
}

func TestIncompleteSeedFallsBackToDownloading(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes"})
	host, server := t.TempDir(), serve(t, release.dir)
	if _, err := sync(t, release, host, server, ModeTooling, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(installedFile(host, "1.0.0", "alpha"), 1); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: t.TempDir(), Mode: ModeTooling,
		Seeds: []string{filepath.Join(host, "chunks")}, Source: server.URL,
	})
	// alpha's chunk is damaged in the seed; the common chunk is complete.
	if err != nil || result.SeededChunks != 1 || result.DownloadedChunks != 1 {
		t.Fatalf("an incomplete seed chunk must be downloaded: %+v %v", result, err)
	}
}

func TestInstalledBundlesSeedAnotherCacheWithoutAnySource(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes"})
	prepared := t.TempDir()
	if _, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: prepared, Mode: ModeTooling,
		Cache: t.TempDir(), Source: serve(t, release.dir).URL,
	}); err != nil {
		t.Fatal(err)
	}
	// The prepared root has no chunks/ of its own: only manifests/ and installed bundles.
	result, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: t.TempDir(), Mode: ModeTooling,
		Cache: t.TempDir(), Seeds: []string{prepared},
	})
	if err != nil || result.SeededChunks != 2 || result.DownloadedChunks != 0 {
		t.Fatalf("installed bundles must seed chunks offline: %+v %v", result, err)
	}
}

func TestSeedNeverLinksSameSizeFilesOfAnotherVersion(t *testing.T) {
	// Same lengths, different bytes: a size-only seed would install the wrong core.
	newer := writeRelease(t, "1.0.1", map[string]string{"alpha": "alpha-2"})
	older := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-1"})
	installed := t.TempDir()
	if _, err := sync(t, newer, installed, serve(t, newer.dir), ModeTooling, false); err != nil {
		t.Fatal(err)
	}
	root, server := t.TempDir(), serve(t, older.dir)
	result, err := Sync(context.Background(), Options{
		Inputs: older.inputs, Root: root, Mode: ModeTooling,
		Seeds: []string{installed}, Source: server.URL,
	})
	if err != nil || result.SeededChunks != 0 || result.DownloadedChunks != 2 {
		t.Fatalf("seeded mismatching bytes: %+v %v", result, err)
	}
	if contents, _ := os.ReadFile(installedFile(root, "1.0.0", "alpha")); string(contents) != "alpha-1" {
		t.Fatalf("installed the wrong core: %q", contents)
	}
}

func TestWarmSharedCachePreparesAnotherRootWithoutTheNetwork(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes"})
	cache := t.TempDir()
	if _, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: t.TempDir(), Cache: cache,
		Mode: ModeTooling, Source: serve(t, release.dir).URL,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: t.TempDir(), Cache: cache,
		Mode: ModeTooling, Source: "http://127.0.0.1:1",
	})
	if err != nil || result.DownloadedChunks != 0 {
		t.Fatalf("a warm shared cache must not need the network: %+v %v", result, err)
	}
}

func TestAVolumePreparedByAnotherUserStartsWithAReadOnlyLock(t *testing.T) {
	release := writeRelease(t, "1.0.0", map[string]string{"alpha": "alpha-bytes"})
	root := t.TempDir()
	if _, err := sync(t, release, root, serve(t, release.dir), ModeProduction, false); err != nil {
		t.Fatal(err)
	}
	// The preparing user's lock file is not writable for the container user.
	if err := os.Chmod(filepath.Join(root, ".lock"), 0o444); err != nil {
		t.Fatal(err)
	}
	result, err := Sync(context.Background(), Options{
		Inputs: release.inputs, Root: root, Mode: ModeProduction,
		Source: "http://127.0.0.1:1",
	})
	if err != nil || !result.Reused {
		t.Fatalf("a read-only lock must not block a matching start: %+v %v", result, err)
	}
}
