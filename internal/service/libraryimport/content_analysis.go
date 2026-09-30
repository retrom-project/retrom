package libraryimport

import (
	"encoding/json"
	"fmt"

	"retrom/internal/content/arcade"

	corevalidation "retrom/internal/core/validation"
)

// ContentAnalysis retains observations about the imported bytes. BIOS
// installations are resolved when the review is read or used.
type ContentAnalysis struct {
	Status  string          `json:"status"`
	Code    string          `json:"code"`
	Details json.RawMessage `json:"details"`
}

func ContentAnalysisJSON(status, code, dependencies string) (string, error) {
	if snapshot, err := corevalidation.ParseSnapshot(dependencies); err == nil {
		snapshot.BIOS = []corevalidation.BIOSDependency{}
		encoded, err := snapshot.JSON()
		if err != nil {
			return "", fmt.Errorf("encode source observations: %w", err)
		}
		dependencies = string(encoded)
	} else if snapshot, ok := arcade.ParseSnapshot(dependencies); ok {
		encoded, err := json.Marshal(struct {
			Kind    string `json:"kind"`
			Machine string `json:"machine"`
		}{Kind: "ARCADE", Machine: snapshot.Machine})
		if err != nil {
			return "", fmt.Errorf("encode arcade content observation: %w", err)
		}
		dependencies = string(encoded)
	}
	if code == "LAUNCH_BIOS_MISSING" {
		status, code = "READY", "READY"
	}
	encoded, err := json.Marshal(ContentAnalysis{Status: status, Code: code, Details: json.RawMessage(dependencies)})
	if err != nil {
		return "", fmt.Errorf("encode content analysis: %w", err)
	}
	return string(encoded), nil
}
