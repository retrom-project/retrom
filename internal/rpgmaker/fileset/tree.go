package fileset

import (
	"path"
	"slices"
	"strings"
)

// NormalizeTree applies repository-wide path and noise rules without choosing a
// game root. Detection and explicit review selection retain every relative root.
func NormalizeTree(input []SourceFile) (Project, error) {
	files, noise, err := normalizeInputPaths(input)
	if err != nil {
		return Project{}, err
	}
	if err := validateTreeDirectories(files); err != nil {
		return Project{}, err
	}
	slices.SortFunc(files, func(left, right SourceFile) int {
		return strings.Compare(left.Path, right.Path)
	})
	return Project{Files: files, RemovedNoise: noise}, nil
}

func validateTreeDirectories(files []SourceFile) error {
	filePaths := make(map[string]bool, len(files))
	directories := make(map[string]string)
	for _, file := range files {
		filePaths[lookup(file.Path)] = true
	}
	for _, file := range files {
		for directory := path.Dir(file.Path); directory != "."; directory = path.Dir(directory) {
			key := lookup(directory)
			prior, exists := directories[key]
			if filePaths[key] || exists && prior != directory {
				return &ProjectError{Code: CodePathCollision}
			}
			directories[key] = directory
		}
	}
	return nil
}
