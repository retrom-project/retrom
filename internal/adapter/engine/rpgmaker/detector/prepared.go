package detector

import (
	"context"
	"fmt"
	"io"
	"os"

	policy "retrom/internal/capability/engine/rpgmaker/detector"
	model "retrom/internal/model/libraryimport"
)

// PreparedDetector borrows immutable prepared files for the duration of a call.
type PreparedDetector struct{}

var _ model.RPGMakerDetector = PreparedDetector{}

func (PreparedDetector) DetectPrepared(
	_ context.Context, coreID string, files []model.RPGMakerProbeFile,
) (policy.Profile, error) {
	index := preparedIndex{files: make([]policy.File, 0, len(files)), paths: make(map[string]string, len(files))}
	for _, file := range files {
		index.files = append(index.files, file.File)
		index.paths[file.File.Path] = file.SourcePath
	}
	return detect(coreID, index)
}

type preparedIndex struct {
	files []policy.File
	paths map[string]string
}

func (index preparedIndex) Files() []policy.File {
	return append([]policy.File(nil), index.files...)
}

func (index preparedIndex) Open(logicalPath string) (io.ReadCloser, error) {
	localPath, exists := index.paths[logicalPath]
	if !exists {
		return nil, os.ErrNotExist
	}
	reader, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open RPG Maker project file: %w", err)
	}
	return reader, nil
}
