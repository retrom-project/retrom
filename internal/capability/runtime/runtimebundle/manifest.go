// Package runtimebundle parses the versioned Provider bundle boundary.
package runtimebundle

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	runtimejson "retrom/internal/capability/runtime/runtimejson"
	runtimecontract "retrom/internal/model/runtimecontract"
)

var (
	ErrIntegrityInvalid = errors.New("RUNTIME_PROVIDER_INTEGRITY_INVALID")
	errTrailingJSON     = errors.New("trailing JSON")
	errCanonicalNumber  = errors.New("non-canonical number")
	errCanonicalType    = errors.New("unsupported canonical value")
	identityPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	semverPattern       = regexp.MustCompile(
		`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)` +
			`(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)` +
			`(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?$`,
	)
	tokenPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,62}[a-z0-9])?$`)
)

var resourceKinds = map[string]bool{
	"ROM_BLOB": true, "FILE_TREE": true, "SEEKABLE_BLOB": true,
	"NATIVE_WEB": true, "ISOLATED_WEB": true, "BIOS_BUNDLE": true,
	"PARENT_ARCHIVE": true, "MULTI_DISC": true, "EXTERNAL_FILE_SET": true,
	"WASM4_CART": true,
}

var videoModes = map[string]bool{
	"original": true, "pixel": true, "smooth": true,
	"sharp-bilinear": true, "adaptive-sharpen": true,
}

type integrityWire struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Files         []integrityFileWire `json:"files"`
}

type integrityFileWire struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType"`
}

var integrityMediaTypes = map[string]bool{
	"text/javascript; charset=utf-8": true, "text/css; charset=utf-8": true,
	"text/plain; charset=utf-8": true, "application/json; charset=utf-8": true,
	"application/wasm": true, "application/octet-stream": true, "application/zip": true,
	"application/x-7z-compressed": true, "image/png": true, "image/jpeg": true,
	"image/gif": true, "image/webp": true, "image/svg+xml": true, "image/x-icon": true,
	"audio/ogg": true, "audio/mpeg": true, "audio/wav": true, "font/woff": true, "font/woff2": true,
}

func ParseIntegrity(contents []byte) (runtimecontract.Integrity, error) {
	if !validIntegrityRawShape(contents) {
		return runtimecontract.Integrity{}, ErrIntegrityInvalid
	}
	var wire integrityWire
	if err := decodeClosed(contents, &wire); err != nil || wire.SchemaVersion != 1 || len(wire.Files) < 3 {
		return runtimecontract.Integrity{}, ErrIntegrityInvalid
	}
	result := runtimecontract.Integrity{SchemaVersion: 1, Files: make([]runtimecontract.IntegrityFile, 0, len(wire.Files))}
	previous := ""
	for _, file := range wire.Files {
		if !validIntegrityFile(file, previous) {
			return runtimecontract.Integrity{}, ErrIntegrityInvalid
		}
		result.Files = append(result.Files, runtimecontract.IntegrityFile(file))
		previous = file.Path
	}
	return result, nil
}

func validIntegrityRawShape(contents []byte) bool {
	value, err := runtimejson.ParseStrictJSON(contents)
	root, ok := value.(map[string]any)
	if err != nil || !ok || !runtimejson.ExactMap(root, "files", "schemaVersion") {
		return false
	}
	items, ok := root["files"].([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		file, ok := item.(map[string]any)
		if !ok || !runtimejson.ExactMap(file, "mediaType", "path", "sha256", "sizeBytes") {
			return false
		}
	}
	return true
}

func validIntegrityFile(file integrityFileWire, previous string) bool {
	return runtimejson.SafePath(file.Path) && len(file.Path) <= 240 && file.SizeBytes >= 0 &&
		file.SizeBytes <= 9007199254740991 && digestPattern(file.SHA256) &&
		integrityMediaTypes[file.MediaType] && (previous == "" || previous < file.Path)
}

type manifestWire struct {
	SchemaVersion    int               `json:"schemaVersion"`
	ProviderID       string            `json:"providerId"`
	ProviderVersion  string            `json:"providerVersion"`
	ProviderAPI      int               `json:"providerApiVersion"`
	ClientModulePath string            `json:"clientModulePath"`
	Targets          []json.RawMessage `json:"targets"`
}

func ParseManifest(contents []byte) (runtimecontract.Manifest, error) {
	if !validManifestRawShape(contents) {
		return runtimecontract.Manifest{}, runtimecontract.ErrManifestInvalid
	}
	var wire manifestWire
	if err := decodeClosed(contents, &wire); err != nil || wire.SchemaVersion != 1 ||
		!identityPattern.MatchString(wire.ProviderID) || !semverPattern.MatchString(wire.ProviderVersion) ||
		wire.ProviderAPI < 1 || wire.ClientModulePath != "client.mjs" || len(wire.Targets) == 0 {
		return runtimecontract.Manifest{}, invalidManifest(err)
	}
	result := runtimecontract.Manifest{
		SchemaVersion: wire.SchemaVersion, ProviderID: wire.ProviderID,
		ProviderVersion: wire.ProviderVersion, ProviderAPI: wire.ProviderAPI,
		ClientModulePath: wire.ClientModulePath, Targets: make([]runtimecontract.Target, 0, len(wire.Targets)),
	}
	identities := make(map[string]bool, len(wire.Targets))
	previous := ""
	for _, raw := range wire.Targets {
		var target runtimecontract.Target
		if err := decodeClosed(raw, &target); err != nil || !validTarget(target) ||
			identities[target.ID] || previous != "" && previous >= target.ID {
			return runtimecontract.Manifest{}, invalidManifest(err)
		}
		result.Targets = append(result.Targets, target)
		identities[target.ID] = true
		previous = target.ID
	}
	return result, nil
}

func validManifestRawShape(contents []byte) bool {
	value, err := runtimejson.ParseStrictJSON(contents)
	manifest, ok := value.(map[string]any)
	if err != nil || !ok || !runtimejson.ExactMap(manifest,
		"schemaVersion", "providerId", "providerVersion", "providerApiVersion", "clientModulePath", "targets") {
		return false
	}
	targets, ok := manifest["targets"].([]any)
	if !ok {
		return false
	}
	for _, targetValue := range targets {
		if !validManifestRawTarget(targetValue) {
			return false
		}
	}
	return true
}

func validManifestRawTarget(value any) bool {
	target, ok := value.(map[string]any)
	if !ok || !runtimejson.ExactMap(target, "id", "displayName",
		"targetOptionsSchema", "inputs", "capabilities", "checkpoint", "assetPaths") {
		return false
	}
	capabilities, ok := target["capabilities"].(map[string]any)
	if !ok || !runtimejson.ExactMap(capabilities,
		"pause", "screenshot", "checkpoint", "standardGamepad", "frameCounter", "volume", "discSwitch",
		"nativeSettings", "inputFilter", "netplayPort", "videoModes", "requiresThreads", "frameMode") {
		return false
	}
	inputs, ok := target["inputs"].([]any)
	if !ok || !validManifestRawInputs(inputs) {
		return false
	}
	if target["checkpoint"] == nil {
		return true
	}
	checkpoint, ok := target["checkpoint"].(map[string]any)
	return ok && validCheckpointShape(checkpoint)
}

func validManifestRawInputs(inputs []any) bool {
	for _, value := range inputs {
		input, ok := value.(map[string]any)
		if !ok || !runtimejson.ExactMap(input, "role", "kind", "cardinality", "optional") {
			return false
		}
	}
	return true
}

func validTarget(target runtimecontract.Target) bool {
	if !identityPattern.MatchString(target.ID) || len(target.DisplayName) == 0 || len(target.DisplayName) > 120 ||
		!runtimejson.ValidateTargetOptionsSchema(target.TargetOptionsSchema) ||
		!validCapabilities(target.Capabilities) || !validInputs(target.Inputs) || !sortedPaths(target.AssetPaths) {
		return false
	}
	return target.Capabilities.Checkpoint == (target.Checkpoint != nil) &&
		(target.Checkpoint == nil || validCheckpoint(*target.Checkpoint))
}

func validCapabilities(value runtimecontract.Capabilities) bool {
	if value.FrameMode != "NONE" && value.FrameMode != "SAME_ORIGIN_BLANK" &&
		value.FrameMode != "SAME_ORIGIN_RESOURCE" && value.FrameMode != "ISOLATED_ORIGIN_RESOURCE" {
		return false
	}
	return sortedEnum(value.VideoModes, videoModes)
}

func BindTargetIntegrity(
	manifest runtimecontract.Manifest,
	files []runtimecontract.IntegrityFile,
) (runtimecontract.Manifest, error) {
	byPath := make(map[string]runtimecontract.IntegrityFile, len(files))
	for _, file := range files {
		if !runtimejson.SafePath(file.Path) || file.SizeBytes < 0 || !digestPattern(file.SHA256) {
			return runtimecontract.Manifest{}, runtimecontract.ErrManifestInvalid
		}
		if _, exists := byPath[file.Path]; exists {
			return runtimecontract.Manifest{}, runtimecontract.ErrManifestInvalid
		}
		byPath[file.Path] = file
	}
	for _, target := range manifest.Targets {
		for _, path := range target.AssetPaths {
			_, exists := byPath[path]
			if !exists {
				return runtimecontract.Manifest{}, runtimecontract.ErrManifestInvalid
			}
		}
	}
	return manifest, nil
}

func digestPattern(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func sortedEnum(values []string, allowed map[string]bool) bool {
	for index, value := range values {
		if !allowed[value] || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func validInputs(values []runtimecontract.Input) bool {
	if len(values) == 0 {
		return false
	}
	roles := make(map[string]bool, len(values))
	for _, value := range values {
		if !identityPattern.MatchString(value.Role) || roles[value.Role] || !resourceKinds[value.Kind] ||
			(value.Cardinality != "ONE" && value.Cardinality != "MANY") {
			return false
		}
		roles[value.Role] = true
	}
	return true
}

func validCheckpoint(value runtimecontract.Checkpoint) bool {
	if value.MaxBytes <= 0 || !tokenPattern.MatchString(value.WriteFormat) ||
		!sortedTokens(value.ReadFormats, false) || !validCheckpointSemantics(value.Semantics) {
		return false
	}
	for _, format := range value.ReadFormats {
		if format == value.WriteFormat {
			return true
		}
	}
	return false
}

func sortedPaths(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for index, value := range values {
		if !runtimejson.SafePath(value) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func sortedTokens(values []string, empty bool) bool {
	if !empty && len(values) == 0 {
		return false
	}
	for index, value := range values {
		if !tokenPattern.MatchString(value) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func decodeClosed(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode closed JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errTrailingJSON
	}
	return nil
}

func canonicalJSON(contents []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode canonical JSON: %w", err)
	}
	buffer := &bytes.Buffer{}
	if err := appendCanonical(buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func appendCanonical(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		output.WriteString(strconv.FormatBool(typed))
	case string:
		encoded, _ := json.Marshal(typed)
		output.Write(encoded)
	case json.Number:
		integer, err := strconv.ParseInt(typed.String(), 10, 64)
		if err != nil || integer > 9007199254740991 || integer < -9007199254740991 {
			return errCanonicalNumber
		}
		output.WriteString(strconv.FormatInt(integer, 10))
	case []any:
		return appendCanonicalArray(output, typed)
	case map[string]any:
		return appendCanonicalObject(output, typed)
	default:
		return fmt.Errorf("%w: %T", errCanonicalType, value)
	}
	return nil
}

func appendCanonicalArray(output *bytes.Buffer, values []any) error {
	output.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			output.WriteByte(',')
		}
		if err := appendCanonical(output, value); err != nil {
			return err
		}
	}
	output.WriteByte(']')
	return nil
}

func appendCanonicalObject(output *bytes.Buffer, values map[string]any) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return utf16Less(keys[left], keys[right]) })
	output.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			output.WriteByte(',')
		}
		encoded, _ := json.Marshal(key)
		output.Write(encoded)
		output.WriteByte(':')
		if err := appendCanonical(output, values[key]); err != nil {
			return err
		}
	}
	output.WriteByte('}')
	return nil
}

func utf16Less(left, right string) bool {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	for index := 0; index < len(leftUnits) && index < len(rightUnits); index++ {
		if leftUnits[index] != rightUnits[index] {
			return leftUnits[index] < rightUnits[index]
		}
	}
	return len(leftUnits) < len(rightUnits)
}

func invalidManifest(err error) error {
	if err == nil {
		return runtimecontract.ErrManifestInvalid
	}
	return fmt.Errorf("%w: %w", runtimecontract.ErrManifestInvalid, err)
}
