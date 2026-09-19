package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/capability/content/contentprofile"
	butterscotchdetector "retrom/internal/capability/engine/butterscotch/detector"
	nxenginedetector "retrom/internal/capability/engine/nxengine/detector"
	onsdetector "retrom/internal/capability/engine/ons/detector"
	"retrom/internal/capability/engine/rpgmaker/fileset"
	tyranodetector "retrom/internal/capability/engine/tyranoscript/detector"
	model "retrom/internal/model/libraryimport"
)

func (service *ImportPreparation) detectMarkerProject(
	ctx context.Context,
	files []fileset.SourceFile,
	paths map[int]string,
	definition markerProjectDefinition,
) ([]byte, error) {
	probes := make([]model.ProjectProbeFile, 0, len(files))
	for _, file := range files {
		probes = append(probes, model.ProjectProbeFile{
			LogicalPath: file.Path, DeclaredSize: file.SizeBytes, ResourcePath: paths[file.SourceIndex],
		})
	}
	switch contentprofile.ContentKind(definition.contentKind) {
	case contentprofile.ContentKindONSProject:
		return service.detectONSProject(ctx, probes)
	case contentprofile.ContentKindButterscotchProject:
		return service.detectButterscotchProject(ctx, probes)
	case contentprofile.ContentKindNXEngineProject:
		return service.detectNXEngineProject(ctx, probes)
	case contentprofile.ContentKindTyranoScriptProject:
		return detectTyranoScriptProject(files)
	case contentprofile.ContentKindSingleFile, contentprofile.ContentKindDOSBundle,
		contentprofile.ContentKindMultiDisc, contentprofile.ContentKindRPGMakerProject,
		contentprofile.ContentKindKiriKiriProject, contentprofile.ContentKindScummVMProject:
		return nil, model.ErrInvalid
	default:
		return nil, model.ErrInvalid
	}
}

func (service *ImportPreparation) detectONSProject(
	ctx context.Context, files []model.ProjectProbeFile,
) ([]byte, error) {
	if service.onsDetector == nil {
		return nil, model.ErrInvalid
	}
	profile, err := service.onsDetector.Detect(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("detect ONS project: %w", err)
	}
	contents, err := onsdetector.MarshalSnapshot(profile)
	if err != nil {
		return nil, fmt.Errorf("marshal ONS project profile: %w", err)
	}
	return contents, nil
}

func (service *ImportPreparation) detectButterscotchProject(
	ctx context.Context, files []model.ProjectProbeFile,
) ([]byte, error) {
	if service.butterscotchDetector == nil {
		return nil, model.ErrInvalid
	}
	profile, err := service.butterscotchDetector.Detect(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("detect Butterscotch project: %w", err)
	}
	contents, err := butterscotchdetector.MarshalSnapshot(profile)
	if err != nil {
		return nil, fmt.Errorf("marshal Butterscotch project profile: %w", err)
	}
	return contents, nil
}

func (service *ImportPreparation) detectNXEngineProject(
	ctx context.Context, files []model.ProjectProbeFile,
) ([]byte, error) {
	if service.nxengineDetector == nil {
		return nil, model.ErrInvalid
	}
	profile, err := service.nxengineDetector.Detect(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("detect NXEngine project: %w", err)
	}
	contents, err := nxenginedetector.MarshalSnapshot(profile)
	if err != nil {
		return nil, fmt.Errorf("marshal NXEngine project profile: %w", err)
	}
	return contents, nil
}

func detectTyranoScriptProject(files []fileset.SourceFile) ([]byte, error) {
	input := make([]tyranodetector.File, 0, len(files))
	for _, file := range files {
		input = append(input, tyranodetector.File{Path: file.Path, Size: file.SizeBytes})
	}
	profile, err := tyranodetector.Detect(input)
	if err != nil {
		return nil, fmt.Errorf("detect TyranoScript project: %w", err)
	}
	contents, err := tyranodetector.MarshalSnapshot(profile)
	if err != nil {
		return nil, fmt.Errorf("marshal TyranoScript project profile: %w", err)
	}
	return contents, nil
}
