package gamecontent

import (
	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/engine/rpgmaker/detector"
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
	Metadata          blobstore.Metadata
}
