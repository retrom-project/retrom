package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fixtureManifest struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Generator     string                `json:"generator"`
	Files         []fixtureManifestFile `json:"files"`
}

type fixtureManifestFile struct {
	Path       string `json:"path"`
	Generation string `json:"generation"`
	SizeBytes  int64  `json:"sizeBytes"`
	SHA256     string `json:"sha256"`
}

func writeFixtureManifest(output string) error {
	files := make([]fixtureManifestFile, 0)
	err := filepath.WalkDir(output, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(output, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "fixture-manifest.json" {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(contents)
		generation, err := generationForPath(relative)
		if err != nil {
			return err
		}
		files = append(files, fixtureManifestFile{
			Path: relative, Generation: generation, SizeBytes: int64(len(contents)),
			SHA256: hex.EncodeToString(digest[:]),
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("enumerate generated fixture: %w", err)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	manifest := fixtureManifest{SchemaVersion: 1, Generator: "generator/*.go", Files: files}
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode fixture manifest: %w", err)
	}
	contents = append(contents, '\n')
	return writeFile(output, "fixture-manifest.json", contents)
}

func generationForPath(relative string) (string, error) {
	directory, _, found := strings.Cut(relative, "/")
	if !found {
		return "", fmt.Errorf("generated file is outside a generation directory: %s", relative)
	}
	generation := map[string]string{
		"rpg2000": "RPG2000", "rpg2000-compat": "RPG2000", "rpg2003": "RPG2003", "rpgxp": "RPGXP",
		"rpgvx": "RPGVX", "rpgvxace": "RPGVXACE", "rpgmv": "RPGMV",
		"malicious-rpgmv": "RPGMV", "malicious-rpgmz": "RPGMZ",
		"negative-matrix": "SECURITY_MATRIX",
	}[directory]
	if generation == "" {
		return "", fmt.Errorf("unknown generated fixture directory: %s", directory)
	}
	return generation, nil
}
