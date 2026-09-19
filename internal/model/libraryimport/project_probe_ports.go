package libraryimport

import (
	"context"

	butterscotch "retrom/internal/capability/engine/butterscotch/detector"
	nxengine "retrom/internal/capability/engine/nxengine/detector"
	ons "retrom/internal/capability/engine/ons/detector"
)

type ProjectProbeFile struct {
	LogicalPath  string
	DeclaredSize int64
	ResourcePath string
}

type ONSProjectDetector interface {
	Detect(context.Context, []ProjectProbeFile) (ons.Profile, error)
}

type ButterscotchProjectDetector interface {
	Detect(context.Context, []ProjectProbeFile) (butterscotch.Profile, error)
}

type NXEngineProjectDetector interface {
	Detect(context.Context, []ProjectProbeFile) (nxengine.Profile, error)
}
