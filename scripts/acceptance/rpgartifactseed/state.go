package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var (
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func validatePaths(databasePath, statePath string, preparing bool) error {
	if !filepath.IsAbs(databasePath) || !filepath.IsAbs(statePath) || filepath.Clean(databasePath) == filepath.Clean(statePath) {
		return errors.New("ACC_RPG_012_PATHS_MUST_BE_DISTINCT_ABSOLUTE_PATHS")
	}
	for _, path := range []string{databasePath, statePath} {
		info, err := os.Lstat(path)
		if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("ACC_RPG_012_UNSAFE_PATH: %s", filepath.Base(path))
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect acceptance path: %w", err)
		}
	}
	if preparing {
		if _, err := os.Lstat(statePath); err == nil || !errors.Is(err, os.ErrNotExist) {
			return errors.New("ACC_RPG_012_STATE_ALREADY_EXISTS")
		}
	}
	return nil
}

func databasePathDigest(path string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(digest[:])
}

func readState(path, databasePath string) (seedState, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return seedState{}, fmt.Errorf("read acceptance state: %w", err)
	}
	var state seedState
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return seedState{}, fmt.Errorf("decode acceptance state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return seedState{}, errors.New("ACC_RPG_012_STATE_TRAILING_DATA")
	}
	if err := validateState(state, databasePath); err != nil {
		return seedState{}, err
	}
	return state, nil
}

func validateState(state seedState, databasePath string) error {
	if state.SchemaVersion != stateVersion || state.CaseID != caseID ||
		state.DatabasePathSHA256 != databasePathDigest(databasePath) || state.UpdatedAtMS < 0 {
		return errors.New("ACC_RPG_012_STATE_IDENTITY_INVALID")
	}
	if state.Phase != phaseOldSelected && state.Phase != phaseNewSelected && state.Phase != phaseDriftSeeded {
		return errors.New("ACC_RPG_012_STATE_PHASE_INVALID")
	}
	if err := validateArtifact(state.OldArtifact); err != nil {
		return err
	}
	if err := validateArtifact(state.NewArtifact); err != nil {
		return err
	}
	if state.OldArtifact.ID == state.NewArtifact.ID || state.OldArtifact.CoreID != state.NewArtifact.CoreID ||
		state.OldArtifact.Generation != state.NewArtifact.Generation ||
		state.OldArtifact.ArtifactSetSHA256 == state.NewArtifact.ArtifactSetSHA256 {
		return errors.New("ACC_RPG_012_ARTIFACT_PAIR_INVALID")
	}
	return nil
}

func validateArtifact(artifact artifactBinding) error {
	if !uuidPattern.MatchString(artifact.ID) || artifact.CoreID == "" || artifact.Generation == "" ||
		artifact.RouteKey == "" || artifact.AdapterID == "" || artifact.AdapterABI == "" ||
		!sha256Pattern.MatchString(artifact.ArtifactSetSHA256) || !sha256Pattern.MatchString(artifact.ManifestSHA256) ||
		!artifact.AvailableForLaunch {
		return errors.New("ACC_RPG_012_ARTIFACT_INVALID")
	}
	return nil
}

func writeState(path string, state seedState) error {
	contents, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode acceptance state: %w", err)
	}
	contents = append(contents, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".acc-rpg-012-*.json")
	if err != nil {
		return fmt.Errorf("create acceptance state: %w", err)
	}
	temporaryPath := temporary.Name()
	succeeded := false
	defer func() {
		_ = temporary.Close()
		if !succeeded {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect acceptance state: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write acceptance state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync acceptance state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close acceptance state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish acceptance state: %w", err)
	}
	succeeded = true
	return nil
}
