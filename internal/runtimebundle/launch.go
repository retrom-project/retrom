package runtimebundle

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var ErrLaunchEnvelopeInvalid = errors.New("RUNTIME_PROVIDER_LAUNCH_ENVELOPE_INVALID")

var (
	launchDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	uuidPattern         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	bootstrapPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)
	launchResourceKinds = map[string]bool{
		"ROM_BLOB_V1": true, "SEEKABLE_BLOB_V1": true, "PARENT_ARCHIVE_V1": true, "WASM4_CART_V1": true,
		"FILE_TREE_V1": true, "NATIVE_WEB_V1": true, "ISOLATED_WEB_V1": true,
		"BIOS_BUNDLE_V1": true, "EXTERNAL_FILE_SET_V1": true, "MULTI_DISC_V1": true,
	}
)

// ParseLaunchEnvelope applies the raw JSON and closed semantic V1 boundary.
func ParseLaunchEnvelope(contents []byte) (map[string]any, error) {
	value, err := parseStrictJSON(contents)
	if err != nil {
		return nil, invalidLaunch(err)
	}
	envelope, ok := value.(map[string]any)
	if !ok || !validLaunchEnvelope(envelope) {
		return nil, ErrLaunchEnvelopeInvalid
	}
	return envelope, nil
}

func validLaunchEnvelope(value map[string]any) bool {
	if !exactMap(value, "netplay", "resources", "restore", "runtime", "schemaVersion", "session", "targetOptions", "validation") ||
		value["schemaVersion"] != int64(1) {
		return false
	}
	session, ok := launchObject(value["session"])
	if !ok || !validLaunchSession(session) {
		return false
	}
	runtime, ok := launchObject(value["runtime"])
	if !ok || !validLaunchRuntime(runtime) || !validLaunchResources(value["resources"]) ||
		!validLaunchOptions(value["targetOptions"]) {
		return false
	}
	capabilities, _ := launchObject(runtime["capabilities"])
	checkpoint := runtime["checkpoint"]
	return validLaunchRestore(value["restore"], checkpoint) &&
		validLaunchValidation(value["validation"], capabilities) &&
		validLaunchNetplay(value["netplay"], capabilities, session)
}

func validLaunchSession(value map[string]any) bool {
	if !exactMap(value, "id", "mode", "platformName", "purpose", "returnTo", "title", "warnings") ||
		!uuidPattern.MatchString(stringValue(value["id"])) ||
		!oneOf(stringValue(value["purpose"]), "PRODUCT", "REVIEW_PREVIEW", "RUNTIME_VALIDATION") ||
		!oneOf(stringValue(value["mode"]), "SINGLE", "NETPLAY") ||
		!boundedString(value["title"], 1, 500) || !boundedString(value["platformName"], 1, 200) ||
		!relativeURLValue(value["returnTo"]) {
		return false
	}
	warnings, ok := launchArray(value["warnings"])
	if !ok || len(warnings) > 16 {
		return false
	}
	for _, warning := range warnings {
		if !boundedString(warning, 1, 200) {
			return false
		}
	}
	return true
}

func validLaunchRuntime(value map[string]any) bool {
	if !exactMap(value, "bundleSha256", "capabilities", "checkpoint", "gameCompatibilityLine", "moduleSha256", "moduleUrl",
		"providerApiVersion", "providerId", "providerVersion", "runtimeBaseUrl", "targetContractSha256", "targetId") ||
		value["providerApiVersion"] != int64(1) || !identityPattern.MatchString(stringValue(value["providerId"])) ||
		!identityPattern.MatchString(stringValue(value["targetId"])) || !semverPattern.MatchString(stringValue(value["providerVersion"])) ||
		!tokenPattern.MatchString(stringValue(value["gameCompatibilityLine"])) ||
		!launchDigestPattern.MatchString(stringValue(value["bundleSha256"])) ||
		!launchDigestPattern.MatchString(stringValue(value["moduleSha256"])) ||
		!launchDigestPattern.MatchString(stringValue(value["targetContractSha256"])) {
		return false
	}
	capabilities, ok := launchObject(value["capabilities"])
	if !ok || !validLaunchCapabilities(capabilities) {
		return false
	}
	checkpointEnabled, _ := capabilities["checkpoint"].(bool)
	if checkpointEnabled != (value["checkpoint"] != nil) || checkpointEnabled && !validLaunchCheckpoint(value["checkpoint"]) {
		return false
	}
	base := fmt.Sprintf("/runtime/providers/%s/%s/", value["providerId"], value["bundleSha256"])
	return value["runtimeBaseUrl"] == base && value["moduleUrl"] == base+"client.mjs"
}

func validLaunchCapabilities(value map[string]any) bool {
	if !exactMap(value, "checkpoint", "discSwitch", "frameCounter", "frameMode", "inputFilter", "nativeSettings", "netplayPort",
		"pause", "requiresThreads", "screenshot", "standardGamepad", "validationProbes", "videoModes", "volume") {
		return false
	}
	for _, key := range []string{"checkpoint", "discSwitch", "frameCounter", "inputFilter", "nativeSettings", "netplayPort",
		"pause", "requiresThreads", "screenshot", "standardGamepad", "volume"} {
		if _, ok := value[key].(bool); !ok {
			return false
		}
	}
	if !oneOf(stringValue(value["frameMode"]), "NONE", "SAME_ORIGIN_BLANK", "SAME_ORIGIN_RESOURCE", "ISOLATED_ORIGIN_RESOURCE") {
		return false
	}
	probes, ok := launchStringSet(value["validationProbes"], true)
	if !ok {
		return false
	}
	for _, probe := range probes {
		if !tokenPattern.MatchString(probe) {
			return false
		}
	}
	modes, ok := launchStringSet(value["videoModes"], true)
	if !ok {
		return false
	}
	for _, mode := range modes {
		if !videoModes[mode] {
			return false
		}
	}
	return true
}

func validLaunchCheckpoint(value any) bool {
	checkpoint, ok := launchObject(value)
	if !ok || !exactMap(checkpoint, "maxBytes", "readFormats", "writeFormat") ||
		!positiveLaunchInteger(checkpoint["maxBytes"]) || !tokenPattern.MatchString(stringValue(checkpoint["writeFormat"])) {
		return false
	}
	formats, ok := launchStringSet(checkpoint["readFormats"], false)
	if !ok {
		return false
	}
	for _, format := range formats {
		if !tokenPattern.MatchString(format) {
			return false
		}
		if format == checkpoint["writeFormat"] {
			return true
		}
	}
	return false
}

func validLaunchResources(value any) bool {
	resources, ok := launchArray(value)
	if !ok || len(resources) > 128 {
		return false
	}
	ordinals := make(map[string][]int64)
	for _, item := range resources {
		resource, ok := launchObject(item)
		role := stringValue(resource["role"])
		ordinal, ordinalOK := nonNegativeLaunchInteger(resource["ordinal"])
		kind := stringValue(resource["kind"])
		if !ok || !identityPattern.MatchString(role) || !ordinalOK || !launchResourceKinds[kind] ||
			!validLaunchResourceShape(resource, kind) {
			return false
		}
		ordinals[role] = append(ordinals[role], ordinal)
	}
	for _, values := range ordinals {
		for index, ordinal := range values {
			if ordinal != int64(index) {
				return false
			}
		}
	}
	return true
}

func validLaunchResourceShape(value map[string]any, kind string) bool {
	switch kind {
	case "ROM_BLOB_V1", "SEEKABLE_BLOB_V1", "PARENT_ARCHIVE_V1", "WASM4_CART_V1":
		rangeRequired, ok := value["rangeRequired"].(bool)
		return exactMap(value, "kind", "ordinal", "rangeRequired", "role", "sha256", "sizeBytes", "url") && ok &&
			launchDigestPattern.MatchString(stringValue(value["sha256"])) && positiveLaunchInteger(value["sizeBytes"]) &&
			relativeURLValue(value["url"]) && rangeRequired == (kind == "SEEKABLE_BLOB_V1" || kind == "PARENT_ARCHIVE_V1")
	case "FILE_TREE_V1":
		return exactMap(value, "contentDigest", "indexUrl", "kind", "ordinal", "role") &&
			launchDigestPattern.MatchString(stringValue(value["contentDigest"])) && relativeURLValue(value["indexUrl"])
	case "NATIVE_WEB_V1", "ISOLATED_WEB_V1":
		origin := stringValue(value["origin"])
		return exactMap(value, "bootstrapTicket", "cleanupUrl", "contentDigest", "entryUrl", "kind", "ordinal", "origin", "role") &&
			launchDigestPattern.MatchString(stringValue(value["contentDigest"])) && validOrigin(origin) &&
			sameOriginURL(value["entryUrl"], origin) && (value["cleanupUrl"] == nil || sameOriginURL(value["cleanupUrl"], origin)) &&
			bootstrapPattern.MatchString(stringValue(value["bootstrapTicket"]))
	case "BIOS_BUNDLE_V1", "EXTERNAL_FILE_SET_V1":
		if !exactMap(value, "files", "kind", "ordinal", "role") {
			return false
		}
		files, ok := launchArray(value["files"])
		paths := make([]string, 0, len(files))
		if !ok || len(files) == 0 {
			return false
		}
		for _, item := range files {
			file, ok := launchObject(item)
			if !ok || !exactMap(file, "logicalName", "sha256", "sizeBytes", "url", "virtualPath") ||
				!boundedString(file["logicalName"], 1, 240) || !safePath(stringValue(file["virtualPath"])) ||
				!relativeURLValue(file["url"]) || !launchDigestPattern.MatchString(stringValue(file["sha256"])) ||
				!positiveLaunchInteger(file["sizeBytes"]) {
				return false
			}
			paths = append(paths, stringValue(file["virtualPath"]))
		}
		return sortedStrings(paths, false)
	case "MULTI_DISC_V1":
		if !exactMap(value, "entries", "initialDiscIndex", "kind", "ordinal", "role") {
			return false
		}
		entries, ok := launchArray(value["entries"])
		initial, initialOK := nonNegativeLaunchInteger(value["initialDiscIndex"])
		if !ok || len(entries) == 0 || !initialOK || initial >= int64(len(entries)) {
			return false
		}
		for index, item := range entries {
			entry, ok := launchObject(item)
			if !ok || !exactMap(entry, "index", "label", "sha256", "sizeBytes", "url") || entry["index"] != int64(index) ||
				!boundedString(entry["label"], 1, 240) || !relativeURLValue(entry["url"]) ||
				!launchDigestPattern.MatchString(stringValue(entry["sha256"])) || !positiveLaunchInteger(entry["sizeBytes"]) {
				return false
			}
		}
		return true
	}
	return false
}

func validLaunchOptions(value any) bool {
	options, ok := launchObject(value)
	if !ok {
		return false
	}
	switch options["kind"] {
	case "NONE_V1":
		return exactMap(options, "kind")
	case "EMULATORJS_V1":
		initial, initialOK := nonNegativeLaunchInteger(options["initialDiscIndex"])
		_ = initial
		return exactMap(options, "dosEntryPath", "initialDiscIndex", "kind") &&
			(options["dosEntryPath"] == nil || safePath(stringValue(options["dosEntryPath"]))) &&
			(options["initialDiscIndex"] == nil || initialOK)
	case "RPGMAKER_V1":
		if !exactMap(options, "expectedRestorePosition", "kind") || options["expectedRestorePosition"] == nil {
			return exactMap(options, "expectedRestorePosition", "kind")
		}
		position, ok := launchObject(options["expectedRestorePosition"])
		if !ok || !exactMap(position, "fixtureState", "mapId", "playerX", "playerY") {
			return false
		}
		for _, item := range position {
			if _, ok := nonNegativeLaunchInteger(item); !ok {
				return false
			}
		}
		return true
	case "ONS_PROJECT_V1":
		return exactMap(options, "kind", "scriptEncoding") && oneOf(stringValue(options["scriptEncoding"]), "gbk", "sjis", "utf8")
	case "KIRIKIRI_PROJECT_V1":
		return exactMap(options, "kind", "startupXp3Path") &&
			(options["startupXp3Path"] == nil || safePath(stringValue(options["startupXp3Path"])))
	}
	return false
}

func validLaunchRestore(value, checkpointValue any) bool {
	if value == nil {
		return true
	}
	restore, ok := launchObject(value)
	checkpoint, checkpointOK := launchObject(checkpointValue)
	if !ok || !checkpointOK || !exactMap(restore, "format", "sha256", "sizeBytes", "url") ||
		!launchDigestPattern.MatchString(stringValue(restore["sha256"])) || !positiveLaunchInteger(restore["sizeBytes"]) ||
		!relativeURLValue(restore["url"]) {
		return false
	}
	formats, ok := launchStringSet(checkpoint["readFormats"], false)
	size, _ := restore["sizeBytes"].(int64)
	maximum, _ := checkpoint["maxBytes"].(int64)
	return ok && contains(formats, stringValue(restore["format"])) && size <= maximum
}

func validLaunchValidation(value any, capabilities map[string]any) bool {
	if value == nil {
		return true
	}
	validation, ok := launchObject(value)
	probes, probesOK := launchStringSet(capabilities["validationProbes"], true)
	return ok && probesOK && exactMap(validation, "input", "probeId") &&
		contains(probes, stringValue(validation["probeId"])) && validJSONValue(validation["input"], 0, true)
}

func validLaunchNetplay(value any, capabilities, session map[string]any) bool {
	if value == nil {
		return session["mode"] != "NETPLAY"
	}
	netplay, ok := launchObject(value)
	port, _ := capabilities["netplayPort"].(bool)
	player, playerOK := netplay["playerNo"].(int64)
	return ok && port && session["mode"] == "NETPLAY" &&
		exactMap(netplay, "playerNo", "profile", "roomId", "sessionId", "socketUrl") &&
		boundedString(netplay["roomId"], 1, 128) && uuidPattern.MatchString(stringValue(netplay["sessionId"])) &&
		playerOK && player >= 1 && player <= 16 && validWebSocketURL(netplay["socketUrl"]) &&
		validJSONValue(netplay["profile"], 0, true)
}

func validJSONValue(value any, depth int, requireObject bool) bool {
	if depth > 8 {
		return false
	}
	if requireObject {
		_, ok := value.(map[string]any)
		if !ok {
			return false
		}
	}
	switch typed := value.(type) {
	case nil, bool, string, int64:
		return !requireObject
	case []any:
		if requireObject || len(typed) > 256 {
			return false
		}
		for _, item := range typed {
			if !validJSONValue(item, depth+1, false) {
				return false
			}
		}
		return true
	case map[string]any:
		if len(typed) > 64 {
			return false
		}
		for _, item := range typed {
			if !validJSONValue(item, depth+1, false) {
				return false
			}
		}
		return true
	}
	return false
}

func exactMap(value map[string]any, keys ...string) bool {
	if len(value) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, exists := value[key]; !exists {
			return false
		}
	}
	return true
}

func launchObject(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}
func launchArray(value any) ([]any, bool) { result, ok := value.([]any); return result, ok }
func stringValue(value any) string        { result, _ := value.(string); return result }
func positiveLaunchInteger(value any) bool {
	integer, ok := value.(int64)
	return ok && integer > 0 && integer <= 9007199254740991
}
func nonNegativeLaunchInteger(value any) (int64, bool) {
	integer, ok := value.(int64)
	return integer, ok && integer >= 0 && integer <= 9007199254740991
}
func boundedString(value any, minimum, maximum int) bool {
	text, ok := value.(string)
	return ok && utf8.RuneCountInString(text) >= minimum && utf8.RuneCountInString(text) <= maximum
}
func relativeURLValue(value any) bool {
	text, ok := value.(string)
	if !ok || len(text) < 2 || len(text) > 2048 || !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "//") ||
		strings.ContainsAny(text, "\\#") {
		return false
	}
	for _, character := range text {
		if character < ' ' || character > '~' {
			return false
		}
	}
	return true
}
func validOrigin(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" &&
		parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.String() == value
}
func sameOriginURL(value any, origin string) bool {
	text, ok := value.(string)
	parsed, err := url.Parse(text)
	base, baseErr := url.Parse(origin)
	return ok && err == nil && baseErr == nil && parsed.Fragment == "" && parsed.Scheme == base.Scheme && parsed.Host == base.Host
}
func validWebSocketURL(value any) bool {
	text, ok := value.(string)
	parsed, err := url.Parse(text)
	return ok && len(text) <= 2048 && err == nil && (parsed.Scheme == "ws" || parsed.Scheme == "wss") &&
		parsed.Host != "" && parsed.Fragment == ""
}
func launchStringSet(value any, allowEmpty bool) ([]string, bool) {
	items, ok := launchArray(value)
	if !ok || !allowEmpty && len(items) == 0 {
		return nil, false
	}
	result := make([]string, len(items))
	for index, item := range items {
		result[index] = stringValue(item)
		if result[index] == "" && item != "" {
			return nil, false
		}
	}
	return result, sortedStrings(result, allowEmpty)
}
func sortedStrings(values []string, allowEmpty bool) bool {
	if !allowEmpty && len(values) == 0 {
		return false
	}
	for index, value := range values {
		if index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}
func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
func oneOf(value string, values ...string) bool { return contains(values, value) }

type strictJSONParser struct {
	contents []byte
	offset   int
}

func parseStrictJSON(contents []byte) (any, error) {
	if !utf8.Valid(contents) {
		return nil, errors.New("invalid UTF-8")
	}
	parser := &strictJSONParser{contents: contents}
	value, err := parser.value()
	parser.space()
	if err != nil || parser.offset != len(contents) {
		return nil, errors.New("invalid canonical JSON subset")
	}
	return value, nil
}

func (parser *strictJSONParser) value() (any, error) {
	parser.space()
	if parser.offset >= len(parser.contents) {
		return nil, errors.New("missing JSON value")
	}
	switch parser.contents[parser.offset] {
	case '"':
		return parser.string()
	case '{':
		return parser.object()
	case '[':
		return parser.array()
	case 't':
		return parser.literal("true", true)
	case 'f':
		return parser.literal("false", false)
	case 'n':
		return parser.literal("null", nil)
	default:
		return parser.integer()
	}
}

func (parser *strictJSONParser) object() (any, error) {
	parser.offset++
	parser.space()
	result := map[string]any{}
	if parser.take('}') {
		return result, nil
	}
	for {
		key, err := parser.string()
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[key]; duplicate {
			return nil, errors.New("duplicate JSON field")
		}
		parser.space()
		if !parser.take(':') {
			return nil, errors.New("missing object colon")
		}
		value, err := parser.value()
		if err != nil {
			return nil, err
		}
		result[key] = value
		parser.space()
		if parser.take('}') {
			return result, nil
		}
		if !parser.take(',') {
			return nil, errors.New("missing object comma")
		}
		parser.space()
	}
}

func (parser *strictJSONParser) array() (any, error) {
	parser.offset++
	parser.space()
	result := []any{}
	if parser.take(']') {
		return result, nil
	}
	for {
		value, err := parser.value()
		if err != nil {
			return nil, err
		}
		result = append(result, value)
		parser.space()
		if parser.take(']') {
			return result, nil
		}
		if !parser.take(',') {
			return nil, errors.New("missing array comma")
		}
		parser.space()
	}
}

func (parser *strictJSONParser) string() (string, error) {
	if !parser.take('"') {
		return "", errors.New("missing string")
	}
	var result strings.Builder
	for parser.offset < len(parser.contents) {
		character := parser.contents[parser.offset]
		if character == '"' {
			parser.offset++
			return result.String(), nil
		}
		if character == '\\' {
			parser.offset++
			if parser.offset >= len(parser.contents) {
				break
			}
			escape := parser.contents[parser.offset]
			parser.offset++
			switch escape {
			case '"', '\\', '/':
				result.WriteByte(escape)
			case 'b':
				result.WriteByte('\b')
			case 'f':
				result.WriteByte('\f')
			case 'n':
				result.WriteByte('\n')
			case 'r':
				result.WriteByte('\r')
			case 't':
				result.WriteByte('\t')
			case 'u':
				first, err := parser.hexUnit()
				if err != nil {
					return "", err
				}
				if first >= 0xd800 && first <= 0xdbff {
					if parser.offset+2 > len(parser.contents) || string(parser.contents[parser.offset:parser.offset+2]) != `\u` {
						return "", errors.New("unpaired surrogate")
					}
					parser.offset += 2
					second, err := parser.hexUnit()
					if err != nil || second < 0xdc00 || second > 0xdfff {
						return "", errors.New("unpaired surrogate")
					}
					result.WriteRune(utf16.DecodeRune(rune(first), rune(second)))
				} else if first >= 0xdc00 && first <= 0xdfff {
					return "", errors.New("unpaired surrogate")
				} else {
					result.WriteRune(rune(first))
				}
			default:
				return "", errors.New("invalid string escape")
			}
			continue
		}
		if character < 0x20 {
			return "", errors.New("control character in string")
		}
		r, size := utf8.DecodeRune(parser.contents[parser.offset:])
		if r == utf8.RuneError && size == 1 || r >= 0xd800 && r <= 0xdfff {
			return "", errors.New("invalid string Unicode")
		}
		result.WriteRune(r)
		parser.offset += size
	}
	return "", errors.New("unterminated string")
}

func (parser *strictJSONParser) hexUnit() (int64, error) {
	if parser.offset+4 > len(parser.contents) {
		return 0, errors.New("short Unicode escape")
	}
	value, err := strconv.ParseInt(string(parser.contents[parser.offset:parser.offset+4]), 16, 32)
	parser.offset += 4
	return value, err
}

func (parser *strictJSONParser) integer() (any, error) {
	start := parser.offset
	if parser.take('-') && parser.offset >= len(parser.contents) {
		return nil, errors.New("missing integer")
	}
	if parser.take('0') {
		if parser.offset < len(parser.contents) && parser.contents[parser.offset] >= '0' && parser.contents[parser.offset] <= '9' {
			return nil, errors.New("integer has leading zero")
		}
	} else {
		if parser.offset >= len(parser.contents) || parser.contents[parser.offset] < '1' || parser.contents[parser.offset] > '9' {
			return nil, errors.New("invalid integer")
		}
		for parser.offset < len(parser.contents) && parser.contents[parser.offset] >= '0' && parser.contents[parser.offset] <= '9' {
			parser.offset++
		}
	}
	value, err := strconv.ParseInt(string(parser.contents[start:parser.offset]), 10, 64)
	if err != nil || value < -9007199254740991 || value > 9007199254740991 {
		return nil, errors.New("integer outside safe range")
	}
	return value, nil
}

func (parser *strictJSONParser) literal(text string, value any) (any, error) {
	if parser.offset+len(text) > len(parser.contents) || string(parser.contents[parser.offset:parser.offset+len(text)]) != text {
		return nil, errors.New("invalid JSON literal")
	}
	parser.offset += len(text)
	return value, nil
}

func (parser *strictJSONParser) space() {
	for parser.offset < len(parser.contents) && bytes.ContainsRune([]byte(" \t\r\n"), rune(parser.contents[parser.offset])) {
		parser.offset++
	}
}
func (parser *strictJSONParser) take(expected byte) bool {
	if parser.offset < len(parser.contents) && parser.contents[parser.offset] == expected {
		parser.offset++
		return true
	}
	return false
}
func invalidLaunch(err error) error { return fmt.Errorf("%w: %v", ErrLaunchEnvelopeInvalid, err) }
