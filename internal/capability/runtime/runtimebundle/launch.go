package runtimebundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	runtimejson "retrom/internal/capability/runtime/runtimejson"
)

var ErrLaunchEnvelopeInvalid = errors.New("RUNTIME_PROVIDER_LAUNCH_ENVELOPE_INVALID")

var (
	launchDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	uuidPattern         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	bootstrapPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)
	launchResourceKinds = map[string]bool{
		"ROM_BLOB": true, "SEEKABLE_BLOB": true, "PARENT_ARCHIVE": true, "WASM4_CART": true,
		"FILE_TREE": true, "NATIVE_WEB": true, "ISOLATED_WEB": true,
		"BIOS_BUNDLE": true, "EXTERNAL_FILE_SET": true, "MULTI_DISC": true,
	}
)

// ParseLaunchEnvelope applies the raw JSON and closed semantic V1 boundary.
func ParseLaunchEnvelope(contents []byte) (map[string]any, error) {
	value, err := runtimejson.ParseStrictJSON(contents)
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
	if !runtimejson.ExactMap(
		value,
		"netplay", "resources", "restore", "runtime", "schemaVersion", "session", "targetOptions",
	) ||
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
		validLaunchNetplay(value["netplay"], capabilities, session)
}

func validLaunchSession(value map[string]any) bool {
	if !runtimejson.ExactMap(
		value, "coreName", "id", "mode", "platformName", "purpose", "returnTo", "title", "warnings",
	) ||
		!uuidPattern.MatchString(stringValue(value["id"])) ||
		!oneOf(stringValue(value["purpose"]), "PRODUCT", "REVIEW_PREVIEW") ||
		!oneOf(stringValue(value["mode"]), "SINGLE", "NETPLAY") ||
		!boundedString(value["title"], 500) || !boundedString(value["platformName"], 200) ||
		!boundedString(value["coreName"], 200) ||
		!relativeURLValue(value["returnTo"]) {
		return false
	}
	warnings, ok := launchArray(value["warnings"])
	if !ok || len(warnings) > 16 {
		return false
	}
	for _, warning := range warnings {
		if !boundedString(warning, 200) {
			return false
		}
	}
	return true
}

func validLaunchRuntime(value map[string]any) bool {
	if !runtimejson.ExactMap(value, "bundleSha256", "capabilities", "checkpoint", "moduleSha256", "moduleUrl",
		"providerApiVersion", "providerId", "providerVersion", "runtimeBaseUrl", "targetId") ||
		!validLaunchRuntimeIdentity(value) {
		return false
	}
	capabilities, ok := launchObject(value["capabilities"])
	if !ok || !validLaunchCapabilities(capabilities) {
		return false
	}
	checkpointEnabled, _ := capabilities["checkpoint"].(bool)
	if checkpointEnabled != (value["checkpoint"] != nil) ||
		checkpointEnabled && !validLaunchCheckpoint(value["checkpoint"]) {
		return false
	}
	base := fmt.Sprintf("/runtime/providers/%s/%s/", value["providerId"], value["bundleSha256"])
	return value["runtimeBaseUrl"] == base && value["moduleUrl"] == base+"client.mjs"
}

func validLaunchRuntimeIdentity(value map[string]any) bool {
	return value["providerApiVersion"] == int64(1) &&
		identityPattern.MatchString(stringValue(value["providerId"])) &&
		identityPattern.MatchString(stringValue(value["targetId"])) &&
		semverPattern.MatchString(stringValue(value["providerVersion"])) &&
		launchDigestPattern.MatchString(stringValue(value["bundleSha256"])) &&
		launchDigestPattern.MatchString(stringValue(value["moduleSha256"]))
}

func validLaunchCapabilities(value map[string]any) bool {
	if !runtimejson.ExactMap(
		value,
		"checkpoint", "discSwitch", "frameCounter", "frameMode", "inputFilter", "nativeSettings", "netplayPort",
		"pause", "requiresThreads", "screenshot", "standardGamepad", "videoModes", "volume") {
		return false
	}
	for _, key := range []string{
		"checkpoint", "discSwitch", "frameCounter", "inputFilter", "nativeSettings", "netplayPort",
		"pause", "requiresThreads", "screenshot", "standardGamepad", "volume",
	} {
		if _, ok := value[key].(bool); !ok {
			return false
		}
	}
	if !oneOf(stringValue(value["frameMode"]),
		"NONE", "SAME_ORIGIN_BLANK", "SAME_ORIGIN_RESOURCE", "ISOLATED_ORIGIN_RESOURCE") {
		return false
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
	if !ok || !validCheckpointShape(checkpoint) ||
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
	case "ROM_BLOB", "SEEKABLE_BLOB", "PARENT_ARCHIVE", "WASM4_CART":
		return validBlobResource(value, kind)
	case "FILE_TREE":
		return runtimejson.ExactMap(value, "contentDigest", "indexUrl", "kind", "ordinal", "role") &&
			launchDigestPattern.MatchString(stringValue(value["contentDigest"])) && relativeURLValue(value["indexUrl"])
	case "NATIVE_WEB", "ISOLATED_WEB":
		return validWebResource(value)
	case "BIOS_BUNDLE", "EXTERNAL_FILE_SET":
		return validFileSetResource(value)
	case "MULTI_DISC":
		return validMultiDiscResource(value)
	}
	return false
}

func validBlobResource(value map[string]any, kind string) bool {
	rangeRequired, ok := value["rangeRequired"].(bool)
	return runtimejson.ExactMap(value, "kind", "ordinal", "rangeRequired", "role", "sha256", "sizeBytes", "url") && ok &&
		launchDigestPattern.MatchString(stringValue(value["sha256"])) && positiveLaunchInteger(value["sizeBytes"]) &&
		relativeURLValue(value["url"]) &&
		rangeRequired == (kind == "SEEKABLE_BLOB" || kind == "PARENT_ARCHIVE")
}

func validWebResource(value map[string]any) bool {
	origin := stringValue(value["origin"])
	return runtimejson.ExactMap(value,
		"bootstrapTicket", "cleanupUrl", "contentDigest", "entryUrl", "kind", "ordinal", "origin", "role") &&
		launchDigestPattern.MatchString(stringValue(value["contentDigest"])) && validOrigin(origin) &&
		sameOriginURL(value["entryUrl"], origin) &&
		(value["cleanupUrl"] == nil || sameOriginURL(value["cleanupUrl"], origin)) &&
		bootstrapPattern.MatchString(stringValue(value["bootstrapTicket"]))
}

func validFileSetResource(value map[string]any) bool {
	if !runtimejson.ExactMap(value, "files", "kind", "ordinal", "role") {
		return false
	}
	files, ok := launchArray(value["files"])
	if !ok || len(files) == 0 {
		return false
	}
	paths := make([]string, 0, len(files))
	for _, item := range files {
		file, ok := launchObject(item)
		if !ok || !validFileSetEntry(file) {
			return false
		}
		paths = append(paths, stringValue(file["virtualPath"]))
	}
	return sortedStrings(paths, false)
}

func validFileSetEntry(file map[string]any) bool {
	return runtimejson.ExactMap(file, "logicalName", "sha256", "sizeBytes", "url", "virtualPath") &&
		boundedString(file["logicalName"], 240) && runtimejson.SafePath(stringValue(file["virtualPath"])) &&
		relativeURLValue(file["url"]) && launchDigestPattern.MatchString(stringValue(file["sha256"])) &&
		positiveLaunchInteger(file["sizeBytes"])
}

func validMultiDiscResource(value map[string]any) bool {
	if !runtimejson.ExactMap(value, "entries", "initialDiscIndex", "kind", "ordinal", "role") {
		return false
	}
	entries, ok := launchArray(value["entries"])
	initial, initialOK := nonNegativeLaunchInteger(value["initialDiscIndex"])
	if !ok || len(entries) == 0 || !initialOK || initial >= int64(len(entries)) {
		return false
	}
	for index, item := range entries {
		entry, ok := launchObject(item)
		if !ok || !validMultiDiscEntry(entry, index) {
			return false
		}
	}
	return true
}

func validMultiDiscEntry(entry map[string]any, index int) bool {
	return runtimejson.ExactMap(entry, "index", "label", "sha256", "sizeBytes", "url") && entry["index"] == int64(index) &&
		boundedString(entry["label"], 240) && relativeURLValue(entry["url"]) &&
		launchDigestPattern.MatchString(stringValue(entry["sha256"])) && positiveLaunchInteger(entry["sizeBytes"])
}

func validLaunchOptions(value any) bool {
	options, ok := launchObject(value)
	if !ok || !runtimejson.ValidJSONValue(options, 0, true) {
		return false
	}
	contents, err := json.Marshal(options)
	return err == nil && len(contents) <= runtimejson.MaxTargetOptionsBytes
}

func validLaunchRestore(value, checkpointValue any) bool {
	if value == nil {
		return true
	}
	restore, ok := launchObject(value)
	checkpoint, checkpointOK := launchObject(checkpointValue)
	if !ok || !checkpointOK || !runtimejson.ExactMap(restore, "format", "sha256", "sizeBytes", "url") ||
		!launchDigestPattern.MatchString(stringValue(restore["sha256"])) || !positiveLaunchInteger(restore["sizeBytes"]) ||
		!relativeURLValue(restore["url"]) {
		return false
	}
	formats, ok := launchStringSet(checkpoint["readFormats"], false)
	size, _ := restore["sizeBytes"].(int64)
	maximum, _ := checkpoint["maxBytes"].(int64)
	return ok && contains(formats, stringValue(restore["format"])) && size <= maximum
}

func validLaunchNetplay(value any, capabilities, session map[string]any) bool {
	if value == nil {
		return session["mode"] != "NETPLAY"
	}
	netplay, ok := launchObject(value)
	port, _ := capabilities["netplayPort"].(bool)
	player, playerOK := netplay["playerNo"].(int64)
	return ok && port && session["mode"] == "NETPLAY" &&
		runtimejson.ExactMap(netplay, "playerNo", "profile", "roomId", "sessionId", "socketUrl") &&
		boundedString(netplay["roomId"], 128) && uuidPattern.MatchString(stringValue(netplay["sessionId"])) &&
		playerOK && player >= 1 && player <= 16 && validWebSocketURL(netplay["socketUrl"]) &&
		runtimejson.ValidJSONValue(netplay["profile"], 0, true)
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

func boundedString(value any, maximum int) bool {
	text, ok := value.(string)
	return ok && utf8.RuneCountInString(text) >= 1 && utf8.RuneCountInString(text) <= maximum
}

func relativeURLValue(value any) bool {
	text, ok := value.(string)
	if !ok || len(text) < 1 || len(text) > 2048 || !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "//") ||
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
	return ok && err == nil && baseErr == nil && parsed.Fragment == "" &&
		parsed.Scheme == base.Scheme && parsed.Host == base.Host
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

func invalidLaunch(err error) error { return fmt.Errorf("%w: %w", ErrLaunchEnvelopeInvalid, err) }
