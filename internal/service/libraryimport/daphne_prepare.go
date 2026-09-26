package libraryimport

import (
	"context"

	"retrom/internal/contentprofile"
	daphnedetector "retrom/internal/daphne/detector"
	"retrom/internal/rpgmaker/fileset"
)

var daphneMarkerProject = markerProjectDefinition{
	name: "Daphne", markerSuffixes: []string{".zip"},
	contentKind:       string(contentprofile.ContentKindDaphneProject),
	compatibilityCode: "DAPHNE_RUNTIME_TRIAL_REQUIRED",
	detect: func(files []fileset.SourceFile, paths map[int]string) ([]byte, error) {
		return detectMarkerProject(
			files, paths, "Daphne",
			func(file fileset.SourceFile) daphnedetector.File {
				return daphnedetector.File{Path: file.Path, Size: file.SizeBytes}
			},
			func(index typedProjectIndex[daphnedetector.File]) (daphnedetector.Profile, error) {
				return daphnedetector.Detect(index)
			},
			daphnedetector.MarshalSnapshot,
		)
	},
}

func (service *ImportPreparation) PrepareDaphneProject(
	ctx context.Context, sourceType string, files []ImportFile,
) ([]PreparedDisposition, []PreparedGroup, []PreparedArchive, error) {
	return service.prepareMarkerProject(ctx, sourceType, files, daphneMarkerProject)
}
