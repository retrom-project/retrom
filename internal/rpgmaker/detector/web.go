package detector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
)

var errInvalidSystemJSON = errors.New("invalid System.json structure")

const (
	maxSystemJSONBytes = 8 << 20
	maxIndexHTMLBytes  = 2 << 20
	maxCoreJSBytes     = 16 << 20
	maxJSONDepth       = 256
)

var mvMarkers = []string{
	"index.html", "data/System.json", "js/rpg_core.js", "js/rpg_managers.js",
	"js/rpg_objects.js", "js/rpg_scenes.js", "js/rpg_sprites.js", "js/rpg_windows.js",
	"js/plugins.js", "js/main.js",
}

var mzMarkers = []string{
	"index.html", "data/System.json", "js/rmmz_core.js", "js/rmmz_managers.js",
	"js/rmmz_objects.js", "js/rmmz_scenes.js", "js/rmmz_sprites.js", "js/rmmz_windows.js",
	"js/plugins.js", "js/main.js",
}

func detectWeb(files *catalog) ([]evidence, error) {
	mvComplete := hasAll(files, mvMarkers)
	mzComplete := hasAll(files, mzMarkers) &&
		(files.exists("js/libs/localforage.js") || files.exists("js/libs/localforage.min.js"))
	if hasAll(files, mvMarkers) && mzComplete {
		return []evidence{
			{generation: RPGMV, markers: actualMarkers(files, mvMarkers)},
			{generation: RPGMZ, markers: mzCompleteMarkers(files)},
		}, nil
	}
	if !mvComplete && !mzComplete {
		return nil, nil
	}
	generation := RPGMV
	family := FamilyMV
	markers := actualMarkers(files, mvMarkers)
	corePath := "js/rpg_core.js"
	if mzComplete {
		generation = RPGMZ
		family = FamilyMZ
		markers = mzCompleteMarkers(files)
		corePath = "js/rmmz_core.js"
	}
	systemJSON, err := files.read("data/System.json", maxSystemJSONBytes, CodeWebFormatInvalid)
	if err != nil {
		return nil, err
	}
	if err := validateSystemJSON(systemJSON); err != nil {
		return nil, err
	}
	indexHTML, err := files.read("index.html", maxIndexHTMLBytes, CodeWebFormatInvalid)
	if err != nil {
		return nil, err
	}
	if err := validateIndexHTML(indexHTML, files); err != nil {
		return nil, err
	}
	engineVersion, err := validateJavaScript(files, corePath)
	if err != nil {
		return nil, err
	}
	return []evidence{{
		generation: generation, family: family, markers: markers, engineVersion: engineVersion,
		requirements: []Requirement{RequirementNativeWebIsolation, RequirementRuntimeValidation},
	}}, nil
}

func hasAll(files *catalog, markers []string) bool {
	for _, marker := range markers {
		if !files.exists(marker) {
			return false
		}
	}
	return true
}

func mzCompleteMarkers(files *catalog) []string {
	markers := actualMarkers(files, mzMarkers)
	if files.exists("js/libs/localforage.js") {
		return append(markers, files.original("js/libs/localforage.js"))
	}
	return append(markers, files.original("js/libs/localforage.min.js"))
}

func actualMarkers(files *catalog, markers []string) []string {
	result := make([]string, 0, len(markers))
	for _, marker := range markers {
		result = append(result, files.original(marker))
	}
	return result
}

func validateSystemJSON(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return newError(CodeWebFormatInvalid, "System.json must be one object", err)
	}
	if err := consumeJSONObject(decoder, 1); err != nil {
		return newError(CodeWebFormatInvalid, "invalid System.json", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return newError(CodeWebFormatInvalid, "System.json has trailing content", err)
	}
	return nil
}

func consumeJSONObject(decoder *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("%w: nesting exceeds %d", errInvalidSystemJSON, maxJSONDepth)
	}
	keys := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("read object key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return errInvalidSystemJSON
		}
		if _, duplicate := keys[key]; duplicate {
			return fmt.Errorf("%w: duplicate object key %q", errInvalidSystemJSON, key)
		}
		keys[key] = struct{}{}
		if err := consumeJSONValue(decoder, depth); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return fmt.Errorf("close object: %w", err)
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder, depth int) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("read value: %w", err)
	}
	switch value := token.(type) {
	case json.Delim:
		if value == '{' {
			return consumeJSONObject(decoder, depth+1)
		}
		if value != '[' || depth >= maxJSONDepth {
			return fmt.Errorf("%w: unexpected delimiter %q", errInvalidSystemJSON, value)
		}
		for decoder.More() {
			if err := consumeJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim(']') {
			return fmt.Errorf("close array: %w", closeErr)
		}
	case json.Number:
		number, parseErr := strconv.ParseFloat(string(value), 64)
		if parseErr != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return fmt.Errorf("%w: non-finite number %q", errInvalidSystemJSON, value)
		}
	}
	return nil
}

func validateJavaScript(files *catalog, corePath string) (string, error) {
	engineVersion := ""
	for _, file := range files.paths() {
		extension := strings.ToLower(path.Ext(file.path))
		if extension != ".js" && extension != ".mjs" {
			continue
		}
		contents, err := files.read(file.path, maxCoreJSBytes, CodeWebFormatInvalid)
		if err != nil {
			return "", err
		}
		cleaned := stripJSComments(contents)
		if hasUnsafeBrowserJavaScriptDependency(cleaned) {
			return "", newError(CodeNativeDependencyUnsupported, fmt.Sprintf("unsupported dependency in %q", file.path), nil)
		}
		if lookupKey(file.path) == lookupKey(corePath) {
			engineVersion = parseEngineVersion(cleaned)
		}
	}
	return engineVersion, nil
}

func hasUnsafeBrowserJavaScriptDependency(contents []byte) bool {
	lowered := strings.ToLower(string(contents))
	compact := removeASCIIWhitespace(lowered)
	if containsGlobalJavaScriptCall(compact, "window.open(") {
		return true
	}
	return hasKnownExternalNetworkCall(compact)
}

func containsGlobalJavaScriptCall(contents, call string) bool {
	remaining := contents
	offset := 0
	for {
		position := strings.Index(remaining, call)
		if position < 0 {
			return false
		}
		absolute := offset + position
		if absolute == 0 || !isJavaScriptIdentifierPart(contents[absolute-1]) && contents[absolute-1] != '.' {
			return true
		}
		offset = absolute + len(call)
		remaining = contents[offset:]
	}
}

func isJavaScriptIdentifierPart(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_' || value == '$'
}

func hasKnownExternalNetworkCall(compact string) bool {
	for _, call := range []string{"fetch(", "websocket(", "eventsource(", ".open(", "axios", "$.ajax("} {
		remaining := compact
		for {
			position := strings.Index(remaining, call)
			if position < 0 {
				break
			}
			arguments := remaining[position+len(call):]
			end := strings.IndexByte(arguments, ')')
			if end < 0 || end > 512 {
				end = min(len(arguments), 512)
			}
			if containsExternalURLLiteral(arguments[:end]) {
				return true
			}
			remaining = arguments
		}
	}
	return false
}

func containsExternalURLLiteral(arguments string) bool {
	for _, quote := range []string{`"`, `'`, "`"} {
		for _, scheme := range []string{"http://", "https://", "ws://", "wss://", "//"} {
			if strings.Contains(arguments, quote+scheme) {
				return true
			}
		}
	}
	return false
}

func parseEngineVersion(contents []byte) string {
	for _, line := range bytes.Split(contents, []byte{'\n'}) {
		text := string(line)
		position := strings.Index(text, "Utils.RPGMAKER_VERSION")
		if position < 0 {
			continue
		}
		assignment := strings.TrimSpace(text[position+len("Utils.RPGMAKER_VERSION"):])
		if !strings.HasPrefix(assignment, "=") {
			continue
		}
		assignment = strings.TrimSpace(strings.TrimPrefix(assignment, "="))
		if len(assignment) < 3 || assignment[0] != assignment[len(assignment)-2] ||
			(assignment[0] != '\'' && assignment[0] != '"') || assignment[len(assignment)-1] != ';' {
			continue
		}
		version := assignment[1 : len(assignment)-2]
		if validVersion(version) {
			return version
		}
	}
	return ""
}

func validVersion(version string) bool {
	if version == "" {
		return false
	}
	for _, character := range version {
		if character != '.' && (character < '0' || character > '9') {
			return false
		}
	}
	return !strings.HasPrefix(version, ".") && !strings.HasSuffix(version, ".") && !strings.Contains(version, "..")
}

func removeASCIIWhitespace(value string) string {
	return strings.Map(func(character rune) rune {
		if character == ' ' || character == '\t' || character == '\r' || character == '\n' {
			return -1
		}
		return character
	}, value)
}
