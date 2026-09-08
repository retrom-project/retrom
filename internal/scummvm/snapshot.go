package scummvm

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
)

const ContentKind = "SCUMMVM_PROJECT"

type Snapshot struct {
	SchemaVersion       int    `json:"schemaVersion"`
	Kind                string `json:"kind"`
	Detection           Result `json:"detection"`
	SelectedCandidateID string `json:"selectedCandidateId"`
}

func NewSnapshot(result Result) (Snapshot, error) {
	value := Snapshot{SchemaVersion: 1, Kind: "SCUMMVM", Detection: result, SelectedCandidateID: result.AutomaticSelection}
	if !value.valid() {
		return Snapshot{}, ErrResultInvalid
	}
	return value, nil
}

func ParseSnapshot(raw string) (Snapshot, error) {
	if len(raw) > 16*1024*1024 {
		return Snapshot{}, ErrResultInvalid
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var value Snapshot
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || !value.valid() {
		return Snapshot{}, ErrResultInvalid
	}
	return value, nil
}

func (snapshot Snapshot) valid() bool {
	result := snapshot.Detection
	if snapshot.SchemaVersion != 1 || snapshot.Kind != "SCUMMVM" || !validDigest(result.SourceDigest, 32) ||
		!validDigest(result.UpstreamCommit, 20) || result.Candidates == nil ||
		len(result.Candidates) > 4096 || result.Roots == nil {
		return false
	}
	if !validSnapshotCandidates(result) {
		return false
	}
	if snapshot.SelectedCandidateID != "" {
		if _, err := snapshot.Selected(); err != nil {
			return false
		}
	}
	return true
}

func (snapshot Snapshot) Selected() (Candidate, error) {
	for _, candidate := range snapshot.Detection.Candidates {
		if candidate.ID == snapshot.SelectedCandidateID && candidate.Blocker == "" {
			return candidate, nil
		}
	}
	return Candidate{}, ErrResultInvalid
}

func (snapshot Snapshot) Select(candidateID string) (Snapshot, error) {
	snapshot.SelectedCandidateID = candidateID
	if candidateID == "" || !snapshot.valid() {
		return Snapshot{}, ErrResultInvalid
	}
	return snapshot, nil
}

func (snapshot Snapshot) Status() (string, string) {
	if _, err := snapshot.Selected(); err == nil {
		return "READY", "READY"
	}
	candidates := snapshot.Detection.Candidates
	switch {
	case len(candidates) == 0:
		return "BLOCKED", "SCUMMVM_GAME_NOT_DETECTED"
	case len(snapshot.Detection.Roots) > 1:
		return "BLOCKED", "SCUMMVM_ROOT_SELECTION_REQUIRED"
	case len(candidates) > 1:
		return "BLOCKED", "SCUMMVM_CANDIDATE_SELECTION_REQUIRED"
	default:
		return "BLOCKED", "SCUMMVM_" + candidates[0].Blocker
	}
}

func validSnapshotCandidates(result Result) bool {
	roots := []string{}
	seen := make(map[string]bool, len(result.Candidates))
	for _, candidate := range result.Candidates {
		tool := Tool{UpstreamCommit: result.UpstreamCommit}
		if candidate.Blocker != "ENGINE_UNAVAILABLE" {
			tool.Engines = []string{candidate.EngineID}
		}
		checked, err := checkedCandidate(candidate.DetectedGame, tool, result.SourceDigest)
		if err != nil || checked.ID != candidate.ID || checked.Blocker != candidate.Blocker || seen[candidate.ID] {
			return false
		}
		seen[candidate.ID] = true
		if !slices.Contains(roots, candidate.Root) {
			roots = append(roots, candidate.Root)
		}
	}
	slices.Sort(roots)
	if !slices.Equal(roots, result.Roots) {
		return false
	}
	automatic := ""
	if len(result.Candidates) == 1 && result.Candidates[0].Blocker == "" {
		automatic = result.Candidates[0].ID
	}
	if result.AutomaticSelection != automatic {
		return false
	}
	return true
}
