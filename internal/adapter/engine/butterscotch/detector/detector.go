package detector

import (
	"context"
	"fmt"
	"io"
	"os"

	butterscotch "retrom/internal/capability/engine/butterscotch/detector"
	"retrom/internal/model/diagnostics"
	libraryimport "retrom/internal/model/libraryimport"
)

type Detector struct {
	reporter     diagnostics.ErrorReporter
	openResource func(string) (io.ReadCloser, error)
}

func New(reporter diagnostics.ErrorReporter) *Detector {
	if reporter == nil {
		panic("Butterscotch detector requires a diagnostic reporter")
	}
	return &Detector{reporter: reporter, openResource: openFile}
}

var _ libraryimport.ButterscotchProjectDetector = (*Detector)(nil)

func (detector *Detector) Detect(
	ctx context.Context,
	files []libraryimport.ProjectProbeFile,
) (butterscotch.Profile, error) {
	selection, err := butterscotch.Select(butterscotchFiles(files))
	if err != nil {
		return butterscotch.Profile{}, butterscotch.ErrProjectInvalid
	}
	resourcePath, ok := selectedResource(files, selection.ProbePath())
	if !ok {
		return butterscotch.Profile{}, butterscotch.ErrProjectInvalid
	}
	reader, err := detector.openResource(resourcePath)
	if err != nil || reader == nil {
		return butterscotch.Profile{}, butterscotch.ErrProjectInvalid
	}
	var header [8]byte
	_, readErr := io.ReadFull(reader, header[:])
	closeErr := reader.Close()
	reportClose(ctx, detector.reporter, closeErr)
	if readErr != nil {
		return butterscotch.Profile{}, butterscotch.ErrProjectInvalid
	}
	profile, err := butterscotch.Detect(selection, header)
	if err != nil {
		return butterscotch.Profile{}, butterscotch.ErrProjectInvalid
	}
	return profile, nil
}

func butterscotchFiles(files []libraryimport.ProjectProbeFile) []butterscotch.File {
	result := make([]butterscotch.File, len(files))
	for index, file := range files {
		result[index] = butterscotch.File{Path: file.LogicalPath, Size: file.DeclaredSize}
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
		return nil, fmt.Errorf("open Butterscotch project probe: %w", err)
	}
	return reader, nil
}

func reportClose(ctx context.Context, reporter diagnostics.ErrorReporter, err error) {
	if err != nil {
		reporter.Report(
			ctx,
			diagnostics.CleanupFailure("close butterscotch project probe", "", fmt.Sprintf("%T", err)),
		)
	}
}
