package emulationstationimport

import (
	"database/sql"
	"encoding/json"
	"fmt"

	application "retrom/internal/service/emulationstationimport"
)

func projectRuntimeCheck(
	status, code, coreID, coreName, dependencySnapshot sql.NullString,
) (*application.RuntimeCheck, error) {
	result := &application.RuntimeCheck{
		Status: status.String, Code: code.String, CoreID: coreID.String, CoreName: coreName.String,
		MissingEntries: make([]string, 0), MismatchedEntries: make([]string, 0),
		Dependencies: make([]application.RuntimeDependency, 0), BIOS: make([]application.RuntimeBIOS, 0),
		MissingDiscs: make([]application.RuntimeMissingDisc, 0),
	}
	if !dependencySnapshot.Valid || dependencySnapshot.String == "" {
		return result, nil
	}
	var snapshot struct {
		Machine           *string  `json:"machine"`
		MissingEntries    []string `json:"missingEntries"`
		MismatchedEntries []string `json:"mismatchedEntries"`
		Dependencies      []struct {
			Kind                string   `json:"kind"`
			Machine             string   `json:"machine"`
			RequiredBy          *string  `json:"requiredBy"`
			ExpectedLogicalName string   `json:"expectedLogicalName"`
			State               string   `json:"state"`
			RequiredEntries     []string `json:"requiredEntries"`
		} `json:"dependencies"`
		BIOS []struct {
			LogicalName        string  `json:"logicalName"`
			RequirementMode    string  `json:"requirementMode"`
			ConditionCode      *string `json:"conditionCode"`
			InstallationStatus *string `json:"installationStatus"`
		} `json:"bios"`
		MultiDisc *struct {
			MissingEntries []struct {
				Ordinal         int64  `json:"ordinal"`
				SourceReference string `json:"sourceReference"`
			} `json:"missingEntries"`
		} `json:"multiDisc"`
	}
	if err := json.Unmarshal([]byte(dependencySnapshot.String), &snapshot); err != nil {
		return nil, fmt.Errorf("decode runtime dependency snapshot: %w", err)
	}
	result.Machine = snapshot.Machine
	result.MissingEntries = append(result.MissingEntries, snapshot.MissingEntries...)
	result.MismatchedEntries = append(result.MismatchedEntries, snapshot.MismatchedEntries...)
	for _, dependency := range snapshot.Dependencies {
		result.Dependencies = append(result.Dependencies, application.RuntimeDependency{
			Kind: dependency.Kind, Machine: dependency.Machine, RequiredBy: dependency.RequiredBy,
			ExpectedLogicalName: dependency.ExpectedLogicalName, State: dependency.State,
			RequiredEntries: append([]string(nil), dependency.RequiredEntries...),
		})
		if result.Dependencies[len(result.Dependencies)-1].RequiredEntries == nil {
			result.Dependencies[len(result.Dependencies)-1].RequiredEntries = []string{}
		}
	}
	for _, dependency := range snapshot.BIOS {
		result.BIOS = append(result.BIOS, application.RuntimeBIOS{
			LogicalName: dependency.LogicalName, RequirementMode: dependency.RequirementMode,
			ConditionCode: dependency.ConditionCode, InstallationStatus: dependency.InstallationStatus,
		})
	}
	if snapshot.MultiDisc != nil {
		for _, missing := range snapshot.MultiDisc.MissingEntries {
			result.MissingDiscs = append(result.MissingDiscs, application.RuntimeMissingDisc{
				Ordinal: missing.Ordinal, SourceReference: missing.SourceReference,
			})
		}
	}
	return result, nil
}
