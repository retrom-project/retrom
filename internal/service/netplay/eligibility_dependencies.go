package netplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/transport/netplay/profile"
)

func (service *Eligibility) dependencySnapshotCurrent(ctx context.Context, row EligibilityRow) (bool, error) {
	if row.DATVersionID != nil {
		return service.ArcadeSnapshotRunnable(ctx, row)
	}
	logicalName, rawSnapshot := row.LogicalName, row.DependencyJSON
	lockedJSON, valid := lockedSnapshotJSON(rawSnapshot)
	if !valid {
		return false, nil
	}
	current, status, _, err := service.bios.ResolveBIOS(
		ctx,
		row.ProviderID,
		row.TargetID,
		logicalName,
	)
	if err != nil {
		return false, serviceError("resolve BIOS snapshot", err)
	}
	currentJSON, err := current.JSON()
	if err != nil {
		return false, serviceError("serialize BIOS snapshot", err)
	}
	return status == "READY" && bytes.Equal(lockedJSON, currentJSON), nil
}

type netplayArcadeClosureNode struct {
	Machine    string  `json:"machine"`
	Kind       string  `json:"kind"`
	RequiredBy *string `json:"requiredBy"`
	Depth      int     `json:"depth"`
}

type netplayArcadeDependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy,omitempty"`
	Depth               int      `json:"depth,omitempty"`
	ExpectedLogicalName string   `json:"expectedLogicalName,omitempty"`
	State               string   `json:"state"`
	RequiredEntryCount  int      `json:"requiredEntryCount,omitempty"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type netplayArcadeSnapshot struct {
	SchemaVersion     int                        `json:"schemaVersion"`
	Kind              string                     `json:"kind"`
	Machine           string                     `json:"machine"`
	DATVersionID      string                     `json:"datVersionId"`
	Closure           []netplayArcadeClosureNode `json:"closure"`
	Dependencies      []netplayArcadeDependency  `json:"dependencies"`
	MissingEntries    []string                   `json:"missingEntries"`
	MismatchedEntries []string                   `json:"mismatchedEntries"`
	Warnings          []string                   `json:"warnings"`
}

type netplayLockedArcadeDependency struct {
	state           string
	requiredEntries []string
}

func (service *Eligibility) ArcadeSnapshotRunnable(ctx context.Context, row EligibilityRow) (bool, error) {
	snapshot, valid := parseNetplayArcadeSnapshot(row)
	if !valid {
		return false, nil
	}
	if !validArcadeRuntimeSnapshot(row.DependencyJSON) {
		return false, nil
	}
	closure, valid := netplayArcadeClosureIndex(snapshot)
	if !valid {
		return false, nil
	}
	locked, valid, err := service.loadNetplayArcadeDependencies(ctx, row)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, nil
	}
	if len(snapshot.Dependencies) != len(locked) || len(closure) != len(locked)+1 {
		return false, nil
	}
	return service.arcadeSnapshotDependenciesRunnable(ctx, row.VariantID, snapshot, closure, locked)
}

func validArcadeRuntimeSnapshot(raw string) bool {
	_, err := corevalidation.ParseRuntimeBIOSDependencies(raw)
	return err == nil
}

func parseNetplayArcadeSnapshot(row EligibilityRow) (netplayArcadeSnapshot, bool) {
	var snapshot netplayArcadeSnapshot
	decoder := json.NewDecoder(strings.NewReader(row.DependencyJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil || snapshot.SchemaVersion != corevalidation.SnapshotSchemaVersion ||
		snapshot.Kind != corevalidation.SnapshotKindArcade ||
		snapshot.DATVersionID != *row.DATVersionID || snapshot.Closure == nil || snapshot.Dependencies == nil ||
		snapshot.MissingEntries == nil || len(snapshot.MissingEntries) != 0 || snapshot.MismatchedEntries == nil ||
		len(snapshot.MismatchedEntries) != 0 || snapshot.Warnings == nil ||
		snapshot.Machine != strings.TrimSuffix(filepath.Base(row.LogicalName), filepath.Ext(row.LogicalName)) {
		return netplayArcadeSnapshot{}, false
	}
	return snapshot, true
}

func netplayArcadeClosureIndex(
	snapshot netplayArcadeSnapshot,
) (map[string]netplayArcadeClosureNode, bool) {
	closure := make(map[string]netplayArcadeClosureNode, len(snapshot.Closure))
	for _, node := range snapshot.Closure {
		key := node.Kind + "\x00" + node.Machine
		if node.Machine == "" || (node.Kind != "CONTENT" && node.Kind != "PARENT" && node.Kind != "BIOS_OR_BASE") {
			return nil, false
		}
		if _, duplicate := closure[key]; duplicate {
			return nil, false
		}
		closure[key] = node
	}
	root, exists := closure["CONTENT\x00"+snapshot.Machine]
	if !exists || root.Depth != 0 || root.RequiredBy != nil {
		return nil, false
	}
	return closure, true
}

func (service *Eligibility) loadNetplayArcadeDependencies(
	ctx context.Context, row EligibilityRow,
) (map[string]netplayLockedArcadeDependency, bool, error) {
	rows, err := service.repository.ArcadeDependencies(ctx, row.VariantID, *row.DATVersionID)
	if err != nil {
		return nil, false, serviceError("load Arcade dependencies", err)
	}
	locked := make(map[string]netplayLockedArcadeDependency)
	for _, stored := range rows {
		if stored.LogicalArchive != stored.Machine+".zip" {
			return nil, false, nil
		}
		var entries []string
		validEntries := json.Unmarshal([]byte(stored.RequiredEntriesJSON), &entries) == nil && entries != nil
		if !validEntries {
			return nil, false, nil
		}
		key := stored.Kind + "\x00" + stored.Machine
		if _, duplicate := locked[key]; duplicate {
			return nil, false, nil
		}
		locked[key] = netplayLockedArcadeDependency{state: stored.State, requiredEntries: entries}
	}
	return locked, true, nil
}

func (service *Eligibility) arcadeSnapshotDependenciesRunnable(
	ctx context.Context,
	variantID string,
	snapshot netplayArcadeSnapshot,
	closure map[string]netplayArcadeClosureNode,
	locked map[string]netplayLockedArcadeDependency,
) (bool, error) {
	seenDependencies := make(map[string]struct{}, len(snapshot.Dependencies))
	expectedWarnings := make([]string, 0)
	for _, dependency := range snapshot.Dependencies {
		key := dependency.Kind + "\x00" + dependency.Machine
		node, inClosure := closure[key]
		stored, exists := locked[key]
		if _, duplicate := seenDependencies[key]; duplicate {
			return false, nil
		}
		seenDependencies[key] = struct{}{}
		if !inClosure || !exists || !netplayArcadeDependencyMatches(dependency, node, stored) {
			return false, nil
		}
		if stored.state == "HASH_WARNING" {
			expectedWarnings = append(expectedWarnings, dependency.Machine+".zip:HASH_WARNING")
		}
		if stored.state == "SATISFIED_EXTERNAL" || stored.state == "HASH_WARNING" {
			available, err := service.netplayArcadeDependencyFileAvailable(ctx, variantID, dependency)
			if err != nil || !available {
				return false, err
			}
		}
	}
	sort.Strings(expectedWarnings)
	return slices.Equal(snapshot.Warnings, expectedWarnings), nil
}

func netplayArcadeDependencyMatches(
	dependency netplayArcadeDependency,
	node netplayArcadeClosureNode,
	stored netplayLockedArcadeDependency,
) bool {
	if node.Depth != dependency.Depth ||
		(node.RequiredBy == nil) != (dependency.RequiredBy == nil) ||
		node.RequiredBy != nil && *node.RequiredBy != *dependency.RequiredBy {
		return false
	}
	return dependency.ExpectedLogicalName == dependency.Machine+".zip" &&
		dependency.RequiredEntries != nil && dependency.RequiredEntryCount == len(dependency.RequiredEntries) &&
		slices.Equal(stored.requiredEntries, dependency.RequiredEntries) && stored.state == dependency.State &&
		(stored.state == "SATISFIED_BY_CONTENT" || stored.state == "SATISFIED_EXTERNAL" || stored.state == "HASH_WARNING")
}

func (service *Eligibility) netplayArcadeDependencyFileAvailable(
	ctx context.Context,
	variantID string,
	dependency netplayArcadeDependency,
) (bool, error) {
	role := "BIOS_BUNDLE"
	if dependency.Kind == "PARENT" {
		role = "PARENT"
	}
	count, err := service.repository.DependencyFileCount(ctx, variantID, role, dependency.ExpectedLogicalName)
	if err != nil {
		return false, serviceError("check Arcade dependency file", err)
	}
	if count != 1 {
		return false, nil
	}
	return true, nil
}

func lockedSnapshotJSON(raw string) ([]byte, bool) {
	locked, err := corevalidation.ParseSnapshot(raw)
	if err != nil {
		return nil, false
	}
	encoded, err := locked.JSON()
	return encoded, err == nil
}

func (service *Eligibility) profileEligibility(ctx context.Context, gameID string) ([]EligibleProfile, string, error) {
	lockedRows, err := service.repository.Rows(ctx, gameID)
	if err != nil {
		return nil, "", serviceError("eligible profiles", err)
	}
	result := make([]EligibleProfile, 0)
	seen := make(map[string]struct{})
	contentKindAllowed, coreAllowed := false, false
	for _, row := range lockedRows {
		for _, candidate := range service.registry.Profiles() {
			if _, duplicate := seen[candidate.ID]; duplicate {
				continue
			}
			profile, contentMatch, coreMatch, current, matchErr := service.matchEligibleProfile(ctx, row, candidate)
			contentKindAllowed = contentKindAllowed || contentMatch
			coreAllowed = coreAllowed || coreMatch
			if matchErr != nil {
				return nil, "", matchErr
			}
			if current {
				result = append(result, profile)
				seen[candidate.ID] = struct{}{}
			}
		}
	}
	slices.SortFunc(result, func(left, right EligibleProfile) int {
		return strings.Compare(left.Summary.ID, right.Summary.ID)
	})
	return result, EligibilityBlocker(len(lockedRows) > 0, contentKindAllowed, coreAllowed), nil
}

func (service *Eligibility) matchEligibleProfile(
	ctx context.Context,
	row EligibilityRow,
	candidate profile.ManifestProfile,
) (EligibleProfile, bool, bool, bool, error) {
	contentKindAllowed, targetMatches := service.MatchesTargetProfile(row, candidate)
	if !contentKindAllowed {
		return EligibleProfile{}, false, false, false, nil
	}
	if !targetMatches {
		return EligibleProfile{}, contentKindAllowed, false, false, nil
	}
	current, err := service.dependencySnapshotCurrent(ctx, row)
	if err != nil {
		return EligibleProfile{}, contentKindAllowed, true, false, fmt.Errorf("netplay/dependency snapshot: %w", err)
	}
	return EligibleProfile{
		Summary: ProfileSummary{
			ID: candidate.ID, CoreID: row.CoreID, CoreName: row.CoreName,
			ProviderID: row.ProviderID, TargetID: row.TargetID,
			MaxPlayers: candidate.MaxPlayers,
		},
		Manifest: candidate, VariantID: row.VariantID, BundleSHA256: row.BundleSHA256,
		SourceManifestDigest: row.SourceManifestDigest, DependencySnapshotJSON: row.DependencyJSON,
	}, contentKindAllowed, true, current, nil
}

func (service *Eligibility) MatchesTargetProfile(row EligibilityRow, candidate profile.ManifestProfile) (bool, bool) {
	contentKindAllowed := slices.Contains(service.registry.Manifest.Protocol.AllowedContentKinds, row.ContentKind)
	targetMatches := contentKindAllowed && slices.Contains(candidate.PlatformIDs, row.PlatformID) &&
		candidate.CoreID == row.CoreID && candidate.ProviderID == row.ProviderID &&
		candidate.TargetID == row.TargetID
	return contentKindAllowed, targetMatches
}
