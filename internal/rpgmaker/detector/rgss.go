package detector

import (
	"path"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var rgssSuffixGenerations = map[string]Generation{
	".rxdata": RPGXP, ".rvdata": RPGVX, ".rvdata2": RPGVXAce,
}

var projectMarkerGenerations = map[string]Generation{
	".rxproj": RPGXP, ".rvproj": RPGVX, ".rvproj2": RPGVXAce,
}

var archiveGenerations = map[string]Generation{
	".rgssad": RPGXP, ".rgss2a": RPGVX, ".rgss3a": RPGVXAce,
}

func detectRGSS(files *catalog) ([]evidence, error) {
	hasINI := files.exists("Game.ini")
	hasMarker := hasRGSSFileMarker(files)
	if !hasINI && !hasMarker {
		return nil, nil
	}
	if !hasINI {
		return nil, newError(CodeINIInvalid, "Game.ini is missing", nil)
	}
	contents, err := files.read("Game.ini", maxINIBytes, CodeINIInvalid)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeGameINI(contents)
	if err != nil {
		return nil, err
	}
	values, err := parseGameSection(decoded)
	if err != nil {
		return nil, err
	}
	scriptPath, err := normalizeINIPath(values["scripts"])
	if err != nil {
		return nil, err
	}
	generation, exists := rgssSuffixGenerations[strings.ToLower(path.Ext(scriptPath))]
	if !exists {
		return nil, newError(CodeINIInvalid, "Scripts has an unsupported suffix", nil)
	}
	markers, archiveCount, err := validateRGSSEvidence(files, generation)
	if err != nil {
		return nil, err
	}
	if !files.exists(scriptPath) && archiveCount != 1 {
		return nil, newError(CodeINIInvalid, "Scripts file and matching encrypted archive are absent", nil)
	}
	if err := validateRGSSLibrary(values["library"], generation); err != nil {
		return nil, err
	}
	rtpDependencies := rgssRTPDependencies(values)
	markers = append(markers, files.original("Game.ini"))
	if files.exists(scriptPath) {
		markers = append(markers, files.original(scriptPath))
	}
	return []evidence{{
		generation: generation, family: FamilyRGSS, markers: markers, rtpDependencies: rtpDependencies,
		requirements: []Requirement{RequirementRuntimeValidation},
	}}, nil
}

func parseGameSection(contents string) (map[string]string, error) {
	values := make(map[string]string)
	inGame := false
	gameSections := 0
	for _, rawLine := range strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inGame = strings.EqualFold(strings.TrimSpace(line[1:len(line)-1]), "Game")
			if inGame {
				gameSections++
			}
			continue
		}
		if !inGame {
			continue
		}
		key, value, present := strings.Cut(line, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if !present || key == "" {
			return nil, newError(CodeINIInvalid, "malformed Game.ini assignment", nil)
		}
		if prior, duplicate := values[key]; duplicate && prior != value {
			return nil, newError(CodeINIInvalid, "conflicting duplicate Game.ini key", nil)
		}
		values[key] = value
	}
	if gameSections != 1 || values["scripts"] == "" {
		return nil, newError(CodeINIInvalid, "Game.ini requires exactly one [Game] section and Scripts", nil)
	}
	return values, nil
}

func normalizeINIPath(value string) (string, error) {
	value = norm.NFC.String(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", newError(CodeINIInvalid, "Scripts path is absolute or empty", nil)
	}
	normalized := path.Clean(value)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || !validIndexedPath(normalized) {
		return "", newError(CodeINIInvalid, "Scripts path escapes the project", nil)
	}
	return normalized, nil
}

func validateRGSSEvidence(files *catalog, expected Generation) ([]string, int, error) {
	markers := make([]string, 0, 3)
	archiveCount := 0
	for _, file := range files.paths() {
		if strings.Contains(file.path, "/") {
			continue
		}
		extension := strings.ToLower(path.Ext(file.path))
		generation, projectMarker := projectMarkerGenerations[extension]
		if archiveGeneration, archive := archiveGenerations[extension]; archive {
			generation = archiveGeneration
			archiveCount++
			if archiveCount > 1 {
				return nil, 0, newError(CodeRGSSGenerationConflict, "multiple encrypted RGSS archives", nil)
			}
			projectMarker = true
		}
		if !projectMarker {
			continue
		}
		markers = append(markers, file.path)
		if generation != expected {
			return nil, 0, newError(CodeRGSSGenerationConflict, "RGSS marker generations conflict", nil)
		}
	}
	return markers, archiveCount, nil
}

func validateRGSSLibrary(library string, generation Generation) error {
	if strings.TrimSpace(library) == "" {
		return nil
	}
	want := map[Generation]string{RPGXP: "RGSS1", RPGVX: "RGSS2", RPGVXAce: "RGSS3"}[generation]
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(library)), want) {
		return newError(CodeRGSSGenerationConflict, "Library prefix conflicts with Scripts", nil)
	}
	return nil
}

func rgssRTPDependencies(values map[string]string) []RTPDependency {
	dependencies := make([]RTPDependency, 0, 3)
	for position, key := range []string{"rtp1", "rtp2", "rtp3"} {
		name := strings.TrimSpace(values[key])
		if name == "" {
			continue
		}
		dependencies = append(dependencies, RTPDependency{
			Slot: position + 1, DeclaredName: name,
			NormalizedName: cases.Fold().String(norm.NFKC.String(name)),
		})
	}
	return dependencies
}

func hasRGSSFileMarker(files *catalog) bool {
	for _, file := range files.paths() {
		extension := strings.ToLower(path.Ext(file.path))
		if _, exists := projectMarkerGenerations[extension]; exists {
			return true
		}
		if _, exists := archiveGenerations[extension]; exists {
			return true
		}
	}
	return false
}
