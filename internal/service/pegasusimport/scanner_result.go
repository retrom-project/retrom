package pegasusimport

import model "retrom/internal/model/pegasusimport"

func (result ScanResult) Projection() model.ScanProjection {
	return model.ScanProjection{
		Headers: model.ScanHeaders{Metadata: result.Metadata, Collections: result.Collections},
		Items:   result.Items,
		Summary: model.ScanSummary{
			SnapshotDigest: result.SnapshotDigest, MediaWarnings: result.MediaWarnings,
			Shape: model.ScanShape{
				Metadata: int64(len(result.Metadata)), InvalidMetadata: result.InvalidMetadata,
				Collections: int64(len(result.Collections)), Items: int64(len(result.Items)),
				Blocked: result.Blocked, Covers: result.Covers, Videos: result.Videos,
				EstimatedBytes: result.EstimatedBytes,
			},
		},
	}
}
