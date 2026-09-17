package libraryimport

import (
	"context"
	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/contentprofile"
	nxenginedetector "retrom/internal/capability/engine/nxengine/detector"
	"retrom/internal/capability/engine/rpgmaker/fileset"
)

var nxengineMarkerProject = markerProjectDefinition{
	name: "NXEngine", markers: nxenginedetector.Markers(),
	contentKind:       string(contentprofile.ContentKindNXEngineProject),
	compatibilityCode: "NXENGINE_RUNTIME_TRIAL_REQUIRED",
	detect: func(files []fileset.SourceFile, paths map[int]string) ([]byte, error) {
		return detectMarkerProject(
			files, paths, "NXEngine",
			func(file fileset.SourceFile) nxenginedetector.File {
				return nxenginedetector.File{Path: file.Path, Size: file.SizeBytes}
			},
			func(index typedProjectIndex[nxenginedetector.File]) (nxenginedetector.Profile, error) {
				return nxenginedetector.Detect(index)
			},
			nxenginedetector.MarshalSnapshot,
		)
	},
}

func (service *ImportPreparation) PrepareNXEngineProject(
	ctx context.Context, sourceType string, files []model.ImportFile,
) ([]model.PreparedDisposition, []model.PreparedGroup, []model.PreparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, nxengineMarkerProject)
}
