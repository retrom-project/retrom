package detector

import (
	"context"
	"fmt"
	"io"
	"os"

	nxengine "retrom/internal/capability/engine/nxengine/detector"
	"retrom/internal/model/diagnostics"
	libraryimport "retrom/internal/model/libraryimport"
)

type Detector struct {
	reporter     diagnostics.ErrorReporter
	openResource func(string) (io.ReadCloser, error)
}

func New(reporter diagnostics.ErrorReporter) *Detector {
	if reporter == nil {
		panic("NXEngine detector requires a diagnostic reporter")
	}
	return &Detector{reporter: reporter, openResource: openFile}
}

var _ libraryimport.NXEngineProjectDetector = (*Detector)(nil)

func (detector *Detector) Detect(
	ctx context.Context,
	files []libraryimport.ProjectProbeFile,
) (nxengine.Profile, error) {
	selection, err := nxengine.Select(nxengineFiles(files))
	if err != nil {
		return nxengine.Profile{}, nxengine.ErrProjectInvalid
	}
	resourcePath, ok := selectedResource(files, selection.ProbePath())
	if !ok {
		return nxengine.Profile{}, nxengine.ErrProjectInvalid
	}
	reader, err := detector.openResource(resourcePath)
	if err != nil || reader == nil {
		return nxengine.Profile{}, nxengine.ErrProjectInvalid
	}
	var header [2]byte
	_, readErr := io.ReadFull(reader, header[:])
	closeErr := reader.Close()
	reportClose(ctx, detector.reporter, closeErr)
	if readErr != nil {
		return nxengine.Profile{}, nxengine.ErrProjectInvalid
	}
	profile, err := nxengine.Detect(selection, header)
	if err != nil {
		return nxengine.Profile{}, nxengine.ErrProjectInvalid
	}
	return profile, nil
}

func nxengineFiles(files []libraryimport.ProjectProbeFile) []nxengine.File {
	result := make([]nxengine.File, len(files))
	for index, file := range files {
		result[index] = nxengine.File{Path: file.LogicalPath, Size: file.DeclaredSize}
	}
	return result
}

func selectedResource(files []libraryimport.ProjectProbeFile, logicalPath string) (string, bool) {
	for _, file := range files {
		if file.LogicalPath == logicalPath && file.ResourcePath != "" {
			return file.ResourcePath, true
		}
	}
	return "", false
}

func openFile(resourcePath string) (io.ReadCloser, error) {
	reader, err := os.Open(resourcePath)
	if err != nil {
		return nil, fmt.Errorf("open NXEngine project probe: %w", err)
	}
	return reader, nil
}

func reportClose(ctx context.Context, reporter diagnostics.ErrorReporter, err error) {
	if err != nil {
		reporter.Report(
			ctx,
			diagnostics.CleanupFailure("close nxengine project probe", "", fmt.Sprintf("%T", err)),
		)
	}
}
