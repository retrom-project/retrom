package sourceimport

func (result ScanResult) Projection() ScanProjection {
	diagnostics := make([]ScanDiagnostic, 0)
	for _, metadata := range result.Metadata {
		if metadata.State == "INVALID" && len(diagnostics) < MaxScanDiagnostics {
			diagnostics = append(diagnostics, ScanDiagnostic{
				RelativePath: metadata.Path,
				Line:         metadata.Line, Code: metadata.ErrorCode, Message: metadata.Message,
			})
		}
	}
	return ScanProjection{
		Headers: ScanHeaders{Metadata: result.Metadata, Collections: result.Collections},
		Items:   result.Items,
		Summary: ScanSummary{
			DiscoveredFiles: result.DiscoveredFiles, Diagnostics: diagnostics,
			SnapshotDigest: result.SnapshotDigest, MediaWarnings: result.MediaWarnings,
			Shape: ScanShape{
				Metadata: int64(len(result.Metadata)), InvalidMetadata: result.InvalidMetadata,
				Collections: int64(len(result.Collections)), Items: int64(len(result.Items)),
				Blocked: result.Blocked, Covers: result.Covers, Videos: result.Videos,
				EstimatedBytes: result.EstimatedBytes,
			},
		},
	}
}
