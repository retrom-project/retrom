package sourceimport

const MaxScanDiagnostics = 100

type ScanDiagnostic struct {
	RelativePath string `json:"relativePath"`
	Line         *int64 `json:"line"`
	Code         string `json:"code"`
	Message      string `json:"message"`
}

func (summary ScanSummary) Outcome() string {
	if summary.Shape.Items > 0 && summary.Shape.Collections > 0 {
		if summary.Shape.InvalidMetadata > 0 {
			return "PARTIAL"
		}
		return "READY"
	}
	if summary.Shape.InvalidMetadata > 0 {
		return "INVALID"
	}
	if summary.Shape.Metadata == 0 && summary.DiscoveredFiles > 0 {
		return "NO_METADATA"
	}
	return "EMPTY"
}

func (summary ScanSummary) Completion(nowMS int64) (string, *int64, *string) {
	outcome := summary.Outcome()
	if outcome == "READY" || outcome == "PARTIAL" {
		return "AWAITING_MAPPING", nil, nil
	}
	code := "SOURCE_SCAN_" + outcome
	return "FAILED", &nowMS, &code
}
