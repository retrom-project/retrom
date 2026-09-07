// Package scummvm transports the pinned upstream detector's results. It does not
// maintain a second filename, signature, language or game recognition database.
package scummvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var ErrResultInvalid = errors.New("SCUMMVM_DETECTION_RESULT_INVALID")

var (
	enginePattern = regexp.MustCompile(`^[a-z0-9_]+$`)
	gamePattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

type DetectedGame struct {
	Root            string            `json:"root"`
	EngineID        string            `json:"engineId"`
	GameID          string            `json:"gameId"`
	Description     string            `json:"description"`
	PreferredTarget string            `json:"preferredTarget"`
	Language        string            `json:"language"`
	Platform        string            `json:"platform"`
	Extra           string            `json:"extra"`
	GUIOptions      string            `json:"guiOptions"`
	Config          map[string]string `json:"config"`
	CanBeAdded      bool              `json:"canBeAdded"`
	IsAddOn         bool              `json:"isAddOn"`
	HasUnknownFiles bool              `json:"hasUnknownFiles"`
	SupportLevel    int               `json:"supportLevel"`
}

type Candidate struct {
	DetectedGame
	ID      string `json:"id"`
	Blocker string `json:"blocker"`
}

type Result struct {
	UpstreamCommit     string      `json:"upstreamCommit"`
	SourceDigest       string      `json:"sourceDigest"`
	Candidates         []Candidate `json:"candidates"`
	Roots              []string    `json:"roots"`
	AutomaticSelection string      `json:"automaticSelection"`
}

type detectorResult struct {
	SchemaVersion  int            `json:"schemaVersion"`
	UpstreamCommit string         `json:"upstreamCommit"`
	Candidates     []DetectedGame `json:"candidates"`
	Error          *string        `json:"error"`
}

func parseResult(data []byte, tool Tool, sourceDigest string) (Result, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw detectorResult
	if decoder.Decode(&raw) != nil || decoder.Decode(new(any)) != io.EOF || raw.SchemaVersion != 1 ||
		raw.UpstreamCommit != tool.UpstreamCommit || raw.Error != nil || raw.Candidates == nil ||
		len(raw.Candidates) > 4096 || !validDigest(sourceDigest, 32) || !validDigest(raw.UpstreamCommit, 20) {
		return Result{}, ErrResultInvalid
	}
	result := Result{
		UpstreamCommit: raw.UpstreamCommit, SourceDigest: sourceDigest,
		Candidates: make([]Candidate, 0, len(raw.Candidates)), Roots: []string{},
	}
	for _, detected := range raw.Candidates {
		candidate, err := checkedCandidate(detected, tool, sourceDigest)
		if err != nil {
			return Result{}, err
		}
		result.Candidates = append(result.Candidates, candidate)
		if !slices.Contains(result.Roots, candidate.Root) {
			result.Roots = append(result.Roots, candidate.Root)
		}
	}
	slices.Sort(result.Roots)
	if len(result.Candidates) == 1 && result.Candidates[0].Blocker == "" {
		result.AutomaticSelection = result.Candidates[0].ID
	}
	return result, nil
}

func checkedCandidate(detected DetectedGame, tool Tool, sourceDigest string) (Candidate, error) {
	if !enginePattern.MatchString(detected.EngineID) || !gamePattern.MatchString(detected.GameID) ||
		(detected.Root != "" && !safeRelative(detected.Root)) || detected.SupportLevel < 0 || detected.SupportLevel > 4 ||
		!validHints(detected) {
		return Candidate{}, ErrResultInvalid
	}
	canonical, err := json.Marshal(detected)
	if err != nil {
		return Candidate{}, ErrResultInvalid
	}
	digest := sha256.Sum256(append([]byte(tool.UpstreamCommit+"\n"+sourceDigest+"\n"), canonical...))
	return Candidate{
		DetectedGame: detected, ID: hex.EncodeToString(digest[:]), Blocker: candidateBlocker(detected, tool),
	}, nil
}

func validHints(detected DetectedGame) bool {
	for _, value := range []string{
		detected.Root, detected.Description, detected.PreferredTarget,
		detected.Language, detected.Platform, detected.Extra, detected.GUIOptions,
	} {
		if len(value) > 4096 || strings.ContainsFunc(value, unicode.IsControl) {
			return false
		}
	}
	for key, value := range detected.Config {
		if key != "filename" || !safeRelative(value) {
			return false
		}
	}
	return detected.Config != nil
}

func candidateBlocker(candidate DetectedGame, tool Tool) string {
	switch {
	case candidate.HasUnknownFiles:
		return "UNKNOWN_VARIANT"
	case !candidate.CanBeAdded || candidate.IsAddOn || candidate.SupportLevel == 3:
		return "UNSUPPORTED_GAME"
	case !slices.Contains(tool.Engines, candidate.EngineID):
		return "ENGINE_UNAVAILABLE"
	default:
		return ""
	}
}

func safeRelative(value string) bool {
	return value != "" && value != "." && value != ".." && len(value) <= 4096 &&
		!strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "../") &&
		!strings.Contains(value, "\\") && !strings.ContainsFunc(value, unicode.IsControl) && path.Clean(value) == value
}

func validDigest(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size && value == strings.ToLower(value)
}
