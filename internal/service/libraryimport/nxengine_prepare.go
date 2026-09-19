package libraryimport

import (
	"context"

	"retrom/internal/capability/content/contentprofile"
	nxenginedetector "retrom/internal/capability/engine/nxengine/detector"
	model "retrom/internal/model/libraryimport"
)

var nxengineMarkerProject = markerProjectDefinition{
	name: "NXEngine", markers: nxenginedetector.Markers(),
	contentKind:       string(contentprofile.ContentKindNXEngineProject),
	compatibilityCode: "NXENGINE_RUNTIME_TRIAL_REQUIRED",
}

func (service *ImportPreparation) PrepareNXEngineProject(
	ctx context.Context, sourceType string, files []model.ImportFile,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, nxengineMarkerProject)
}
