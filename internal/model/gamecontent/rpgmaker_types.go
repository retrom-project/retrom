package gamecontent

import (
	"retrom/internal/capability/engine/rpgmaker/detector"
	blobmodel "retrom/internal/model/blob"
)

type PreparedRPGMakerReplacement struct {
	Profile            detector.Profile
	ProjectRoot        string
	ExcludedFiles      []string
	FileCount          int
	TotalBytes         int64
	ProjectFingerprint string
	RequirementsSHA256 string
	AnalysisJSON       []byte
	VariantFiles       []PreparedRPGMakerVariantFile
}

type PreparedRPGMakerVariantFile struct {
	Role, LogicalName string
	Metadata          blobmodel.PreparedBlob
}
