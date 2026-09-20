package libraryimport

import (
	"context"

	"retrom/internal/capability/engine/rpgmaker/detector"
)

// RPGMakerProbeFile identifies one caller-owned input without carrying a resource handle.
type RPGMakerProbeFile struct {
	File       detector.File
	SourcePath string
}

// RPGMakerDetector obtains bounded probe bytes and returns generation evidence.
type RPGMakerDetector interface {
	DetectPrepared(context.Context, string, []RPGMakerProbeFile) (detector.Profile, error)
}
