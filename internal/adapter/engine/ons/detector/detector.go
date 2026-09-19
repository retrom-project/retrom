package detector

import (
	"context"
	"fmt"
	"io"
	"os"

	ons "retrom/internal/capability/engine/ons/detector"
	"retrom/internal/model/diagnostics"
	libraryimport "retrom/internal/model/libraryimport"
)

type Detector struct {
	reporter     diagnostics.ErrorReporter
	openResource func(string) (io.ReadCloser, error)
}

func New(reporter diagnostics.ErrorReporter) *Detector {
	if reporter == nil {
		panic("ONS detector requires a diagnostic reporter")
	}
	return &Detector{reporter: reporter, openResource: openFile}
}

var _ libraryimport.ONSProjectDetector = (*Detector)(nil)

func (detector *Detector) Detect(
	ctx context.Context,
	files []libraryimport.ProjectProbeFile,
) (ons.Profile, error) {
	selection, err := ons.Select(onsFiles(files))
	if err != nil {
		return ons.Profile{}, ons.ErrProjectInvalid
	}
	var probe []byte
	probePath := selection.ProbePath()
	if probePath != "" {
		resourcePath, ok := selectedResource(files, probePath)
		if !ok {
			return ons.Profile{}, scriptUnavailable()
		}
		reader, openErr := detector.openResource(resourcePath)
		if openErr != nil || reader == nil {
			return ons.Profile{}, scriptUnavailable()
		}
		probe, err = io.ReadAll(io.LimitReader(reader, ons.MaxScriptProbeBytes))
		closeErr := reader.Close()
		reportClose(ctx, detector.reporter, closeErr)
		if err != nil {
			return ons.Profile{}, scriptUnavailable()
		}
	}
	profile, err := ons.Detect(selection, probe)
	if err != nil {
		return ons.Profile{}, ons.ErrProjectInvalid
	}
	return profile, nil
}

func onsFiles(files []libraryimport.ProjectProbeFile) []ons.File {
	result := make([]ons.File, len(files))
	for index, file := range files {
		result[index] = ons.File{Path: file.LogicalPath, Size: file.DeclaredSize}
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

func scriptUnavailable() error {
	return fmt.Errorf("%w: script unavailable", ons.ErrProjectInvalid)
}

func openFile(resourcePath string) (io.ReadCloser, error) {
	reader, err := os.Open(resourcePath)
	if err != nil {
		return nil, fmt.Errorf("open ONS project probe: %w", err)
	}
	return reader, nil
}

func reportClose(ctx context.Context, reporter diagnostics.ErrorReporter, err error) {
	if err != nil {
		reporter.Report(ctx, diagnostics.CleanupFailure("close ons project probe", "", fmt.Sprintf("%T", err)))
	}
}
