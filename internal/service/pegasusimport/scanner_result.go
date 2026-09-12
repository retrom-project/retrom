package pegasusimport

func (result ScanResult) Projection() ScanProjection {
	return ScanProjection{
		Headers: ScanHeaders{Metadata: result.Metadata, Collections: result.Collections},
		Items:   result.Items,
		Summary: ScanSummary{
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
