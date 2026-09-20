package gamecontent

import (
	"context"

	"retrom/internal/capability/engine/rpgmaker/detector"
)

// RPGMakerBlobFile identifies one caller-owned input without carrying a resource handle.
type RPGMakerBlobFile struct {
	File   detector.File
	SHA256 string
}

// RPGMakerDetector obtains bounded probe bytes and returns generation evidence.
type RPGMakerDetector interface {
	DetectBlobs(context.Context, string, []RPGMakerBlobFile) (detector.Profile, error)
}
