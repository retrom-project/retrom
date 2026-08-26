package fileset

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"retrom/internal/importing"
	"retrom/internal/rpgmaker/detector"
)

const MaxProjectFiles = 10_000

type Code string

const (
	CodeProjectNotFound Code = "RPG_PROJECT_NOT_FOUND"
	CodeRootAmbiguous   Code = "RPG_PROJECT_ROOT_AMBIGUOUS"
	CodePathCollision   Code = "RPG_PATH_COLLISION"
)

type ProjectError struct {
	Code Code
}

func (projectError *ProjectError) Error() string { return string(projectError.Code) }

type SourceFile struct {
	Path                string
	SizeBytes           int64
	SourceIndex         int
	NestedArchiveFormat importing.NestedArchiveFormat
}

type Project struct {
	Root         string
	Files        []SourceFile
	RemovedNoise []string
}

func NormalizeProject(input []SourceFile) (Project, error) {
	files, noise, err := normalizeInputPaths(input)
	if err != nil {
		return Project{}, err
	}
	files = stripPackagingWrapper(files)
	root, err := chooseProjectRoot(files)
	if err != nil {
		return Project{}, err
	}
	result := make([]SourceFile, 0, len(files))
	for _, file := range files {
		if root == "www" {
			if !strings.HasPrefix(file.Path, "www/") {
				continue
			}
			file.Path = strings.TrimPrefix(file.Path, "www/")
		}
		result = append(result, file)
	}
	projectFiles := append([]SourceFile(nil), result...)
	if err := validateLookupCollisions(projectFiles); err != nil {
		return Project{}, err
	}
	sort.Slice(projectFiles, func(left, right int) bool { return projectFiles[left].Path < projectFiles[right].Path })
	return Project{
		Root: root, Files: projectFiles, RemovedNoise: noise,
	}, nil
}

func ExcludeSessionState(generation detector.Generation, files []SourceFile) ([]SourceFile, []string) {
	included := make([]SourceFile, 0, len(files))
	excluded := make([]string, 0)
	for _, file := range files {
		if sessionStateFile(generation, file.Path) {
			excluded = append(excluded, file.Path)
			continue
		}
		included = append(included, file)
	}
	sort.Strings(excluded)
	return included, excluded
}

func normalizeInputPaths(input []SourceFile) ([]SourceFile, []string, error) {
	files := make([]SourceFile, 0, len(input))
	noise := make([]string, 0)
	for _, file := range input {
		if _, err := importing.ValidateLogicalPath(file.Path); err != nil || file.SizeBytes < 0 {
			return nil, nil, &ProjectError{Code: CodePathCollision}
		}
		if packagingNoise(file.Path) {
			noise = append(noise, file.Path)
			continue
		}
		file.Path = norm.NFC.String(file.Path)
		files = append(files, file)
	}
	if len(files) == 0 || len(files) > MaxProjectFiles {
		return nil, nil, &ProjectError{Code: CodeProjectNotFound}
	}
	if err := validateLookupCollisions(files); err != nil {
		return nil, nil, err
	}
	sort.Strings(noise)
	return files, noise, nil
}

func stripPackagingWrapper(files []SourceFile) []SourceFile {
	firstSegment := ""
	for _, file := range files {
		segment, remainder, nested := strings.Cut(file.Path, "/")
		if !nested || remainder == "" {
			return files
		}
		if firstSegment == "" {
			firstSegment = segment
		} else if segment != firstSegment {
			return files
		}
	}
	result := append([]SourceFile(nil), files...)
	for index := range result {
		result[index].Path = strings.TrimPrefix(result[index].Path, firstSegment+"/")
	}
	return result
}

func chooseProjectRoot(files []SourceFile) (string, error) {
	rootMatch := candidateHasMarker(files, "")
	wwwMatch := candidateHasMarker(files, "www/")
	if rootMatch && wwwMatch {
		return "", &ProjectError{Code: CodeRootAmbiguous}
	}
	if !rootMatch && !wwwMatch {
		return "", &ProjectError{Code: CodeProjectNotFound}
	}
	if wwwMatch {
		return "www", nil
	}
	return ".", nil
}

func candidateHasMarker(files []SourceFile, prefix string) bool {
	top := make(map[string]struct{})
	for _, file := range files {
		if !strings.HasPrefix(file.Path, prefix) {
			continue
		}
		relative := strings.TrimPrefix(file.Path, prefix)
		top[lookup(relative)] = struct{}{}
	}
	has := func(value string) bool { _, exists := top[lookup(value)]; return exists }
	if has("RPG_RT.ldb") || has("RPG_RT.lmt") || has("Game.ini") || hasRootRGSSMarker(top) {
		return true
	}
	hasIndexAndSystem := has("index.html") && has("data/System.json")
	hasIndexAndCore := has("index.html") && (has("js/rpg_core.js") || has("js/rmmz_core.js"))
	return hasIndexAndSystem || hasIndexAndCore
}

func hasRootRGSSMarker(files map[string]struct{}) bool {
	for file := range files {
		if strings.Contains(file, "/") {
			continue
		}
		switch path.Ext(file) {
		case ".rxproj", ".rvproj", ".rvproj2", ".rgssad", ".rgss2a", ".rgss3a":
			return true
		}
	}
	return false
}

func validateLookupCollisions(files []SourceFile) error {
	seen := make(map[string]string, len(files))
	for _, file := range files {
		key := lookup(file.Path)
		if prior, exists := seen[key]; exists {
			return fmt.Errorf("%w: %q conflicts with %q", &ProjectError{Code: CodePathCollision}, prior, file.Path)
		}
		seen[key] = file.Path
	}
	return nil
}

func lookup(value string) string {
	return cases.Fold().String(norm.NFKC.String(value))
}

func packagingNoise(value string) bool {
	segments := strings.Split(value, "/")
	if len(segments) > 0 && strings.EqualFold(segments[0], "__MACOSX") {
		return true
	}
	name := segments[len(segments)-1]
	return name == ".DS_Store" || strings.EqualFold(name, "Thumbs.db") || strings.EqualFold(name, "desktop.ini")
}

var easySavePattern = regexp.MustCompile(`(?i)^save[0-9]+\.lsd$`)

func sessionStateFile(generation detector.Generation, value string) bool {
	if strings.Contains(value, "/") {
		return false
	}
	lower := strings.ToLower(value)
	switch generation {
	case detector.RPG2000, detector.RPG2003:
		return easySavePattern.MatchString(value) || strings.HasSuffix(lower, ".dyn") ||
			lower == "save.lgs" || lower == "easyrpg_log.txt"
	case detector.RPGXP:
		return strings.HasPrefix(lower, "save") && strings.HasSuffix(lower, ".rxdata")
	case detector.RPGVX:
		return strings.HasPrefix(lower, "save") && strings.HasSuffix(lower, ".rvdata")
	case detector.RPGVXAce:
		return strings.HasPrefix(lower, "save") && strings.HasSuffix(lower, ".rvdata2")
	case detector.RPGMV, detector.RPGMZ:
		return false
	default:
		return false
	}
}
