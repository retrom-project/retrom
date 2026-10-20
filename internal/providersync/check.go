package providersync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type damage struct {
	chunks  map[string]bool
	bundles map[string]bool
}

func (d damage) empty() bool { return len(d.chunks) == 0 && len(d.bundles) == 0 }

// check verifies installed data: metadata only by default, file contents with Full.
func (s *syncer) check(manifests []*ChunkManifest) damage {
	started := time.Now()
	bad := damage{chunks: map[string]bool{}, bundles: map[string]bool{}}
	for index, record := range s.inputs.Providers {
		bundle := filepath.Join(s.root, "installed", filepath.FromSlash(installationPath(record)))
		raw, err := os.ReadFile(filepath.Join(bundle, "integrity.json"))
		if err != nil || digest(raw) != record.BundleSHA256 {
			bad.bundles[installationPath(record)] = true
		}
		for _, chunk := range manifests[index].Chunks {
			s.checkChunk(chunk, bundle, record, bad)
		}
	}
	s.result.QuickCheckElapsed = time.Since(started)
	mode := "metadata"
	if s.options.Full {
		mode = "full"
	}
	s.logger.Info("Provider check", "mode", mode, "files", s.result.CheckedFiles,
		"elapsed", s.result.QuickCheckElapsed, "damagedChunks", len(bad.chunks), "damagedBundles", len(bad.bundles))
	return bad
}

func (s *syncer) checkChunk(chunk Chunk, bundle string, record ProviderRecord, bad damage) {
	for _, file := range chunk.Files {
		cached := filepath.Join(s.cache, chunk.ChunkID, filepath.FromSlash(file.Path))
		installed := filepath.Join(bundle, filepath.FromSlash(file.Path))
		s.result.CheckedFiles++
		if !s.fileMatches(cached, file, "") {
			bad.chunks[chunk.ChunkID] = true
			bad.bundles[installationPath(record)] = true
			continue
		}
		if !s.fileMatches(installed, file, cached) {
			bad.bundles[installationPath(record)] = true
		}
	}
}

// fileMatches checks type and size; with Full it also hashes, skipping a hard link of a hashed file.
func (s *syncer) fileMatches(path string, file ChunkFile, hashedTwin string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.SizeBytes {
		return false
	}
	if !s.options.Full {
		return true
	}
	if hashedTwin != "" {
		if twin, twinErr := os.Lstat(hashedTwin); twinErr == nil && os.SameFile(info, twin) {
			return true
		}
	}
	return hashFile(path) == file.SHA256
}

func hashFile(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// discard drops the marker first, so an interrupted repair resumes as a normal sync.
func (s *syncer) discard(bad damage) error {
	if err := os.Remove(filepath.Join(s.root, "ready.json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("invalidate marker: %w", err)
	}
	for chunk := range bad.chunks {
		if err := os.RemoveAll(filepath.Join(s.cache, chunk)); err != nil {
			return fmt.Errorf("remove damaged chunk: %w", err)
		}
		s.result.RepairedChunks++
	}
	for path := range bad.bundles {
		if err := os.RemoveAll(filepath.Join(s.root, "installed", filepath.FromSlash(path))); err != nil {
			return fmt.Errorf("remove damaged bundle: %w", err)
		}
	}
	s.logger.Warn("Provider data damaged; repairing", "chunks", len(bad.chunks), "bundles", len(bad.bundles))
	return nil
}

// cleanup removes bundles, manifests and staging data the marker no longer references.
// Production also removes unreferenced chunks; a shared tooling cache is kept.
func (s *syncer) cleanup(marker *readyMarker) error {
	keepChunks, keepBundles, keepManifests := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for index, provider := range marker.Providers {
		keepBundles[provider.InstallationPath] = true
		keepManifests[s.inputs.Providers[index].ChunkManifest.Asset] = true
		for _, chunk := range provider.Chunks {
			keepChunks[chunk] = true
		}
	}
	removed := 0
	for _, staging := range []string{filepath.Join(s.root, ".staging"), filepath.Join(s.cache, ".staging")} {
		if err := os.RemoveAll(staging); err != nil {
			s.logger.Error("remove Provider staging", "error", err)
		}
	}
	removed += s.removeUnreferenced(filepath.Join(s.root, "manifests"), keepManifests, false)
	bundles, err := os.ReadDir(filepath.Join(s.root, "installed"))
	if err == nil {
		for _, provider := range bundles {
			directory := filepath.Join(s.root, "installed", provider.Name())
			keep := map[string]bool{}
			for path := range keepBundles {
				if id, sum, ok := strings.Cut(path, "/"); ok && id == provider.Name() {
					keep[sum] = true
				}
			}
			s.result.RemovedBundles += s.removeUnreferenced(directory, keep, true)
		}
	}
	if s.options.Mode == ModeProduction {
		s.result.RemovedChunks = s.removeUnreferenced(s.cache, keepChunks, true)
	}
	if removed+s.result.RemovedBundles+s.result.RemovedChunks > 0 {
		s.logger.Info("Removed unreferenced Provider data", "bundles", s.result.RemovedBundles,
			"chunks", s.result.RemovedChunks, "manifests", removed)
	}
	return nil
}

// removeUnreferenced deletes entries of directory that keep does not name. With digestsOnly,
// only 64-hex directory names are candidates, so locks and markers are never touched.
func (s *syncer) removeUnreferenced(directory string, keep map[string]bool, digestsOnly bool) int {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0
	}
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if keep[name] || (digestsOnly && (!entry.IsDir() || !lowerDigest.MatchString(name))) {
			continue
		}
		if err = os.RemoveAll(filepath.Join(directory, name)); err != nil {
			s.logger.Error("remove unreferenced Provider data", "path", name, "error", err)
			continue
		}
		removed++
	}
	return removed
}

// compareVersions orders semantic versions, including prerelease precedence.
func compareVersions(left, right string) int {
	leftCore, leftPre, _ := strings.Cut(left, "-")
	rightCore, rightPre, _ := strings.Cut(right, "-")
	if order := compareFields(strings.Split(leftCore, "."), strings.Split(rightCore, "."), true); order != 0 {
		return order
	}
	switch {
	case leftPre == rightPre:
		return 0
	case leftPre == "":
		return 1
	case rightPre == "":
		return -1
	}
	return compareFields(strings.Split(leftPre, "."), strings.Split(rightPre, "."), false)
}

func compareFields(left, right []string, numeric bool) int {
	for index := 0; index < len(left) && index < len(right); index++ {
		leftNumber, leftErr := strconv.ParseUint(left[index], 10, 64)
		rightNumber, rightErr := strconv.ParseUint(right[index], 10, 64)
		switch {
		case (numeric || (leftErr == nil && rightErr == nil)) && leftNumber != rightNumber:
			if leftNumber < rightNumber {
				return -1
			}
			return 1
		case !numeric && (leftErr == nil) != (rightErr == nil):
			if leftErr == nil {
				return -1
			}
			return 1
		case left[index] != right[index] && (leftErr != nil || rightErr != nil):
			return strings.Compare(left[index], right[index])
		}
	}
	return len(left) - len(right)
}
