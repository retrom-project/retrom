package libraryimport

import (
	"context"

	"retrom/internal/contentprofile"
	nxenginedetector "retrom/internal/nxengine/detector"
	"retrom/internal/rpgmaker/fileset"
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

func (service *Service) prepareNXEngineProject(
	ctx context.Context, sourceType string, files []importSourceFile,
) ([]preparedDisposition, []preparedGroup, []preparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, nxengineMarkerProject)
}
