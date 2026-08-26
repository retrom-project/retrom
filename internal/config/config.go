package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

var (
	errUnknownVariable = errors.New("CONFIG_UNKNOWN_VARIABLE")
	errInvalidConfig   = errors.New("CONFIG_INVALID")
)

var knownVariables = map[string]struct{}{
	"RETROM_HTTP_ADDR": {}, "RETROM_PUBLIC_ORIGIN": {}, "RETROM_DATA_DIR": {},
	"RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN": {},
	"RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE":  {},
	"RETROM_DB_PATH":                      {}, "RETROM_DEPENDENCY_ROOT": {}, "RETROM_DEPENDENCY_VERSIONS": {},
	"RETROM_ACTIVE_EMULATORJS_VERSION": {}, "RETROM_TRUSTED_PROXIES": {},
	"RETROM_STARTUP_CHECK_TIMEOUT": {}, "RETROM_LOG_LEVEL": {},
	"RETROM_MULTI_DISC_IMPORT_ENABLED":    {},
	"RETROM_SERVER_IMPORT_ROOTS":          {},
	"RETROM_NETPLAY_ENABLED":              {},
	"RETROM_NETPLAY_MAX_ACTIVE_ROOMS":     {},
	"RETROM_NETPLAY_ROOM_IDLE_DRAFT_MS":   {},
	"RETROM_NETPLAY_ROOM_IDLE_WAITING_MS": {},
	"RETROM_NETPLAY_RECONNECT_LEASE_MS":   {},
}

var ignoredPrefixes = []string{
	"RETROM_ACCEPTANCE_", "RETROM_CHROME_", "RETROM_EJS_DEP_",
}

type Config struct {
	Mode                     Mode
	HTTPAddr                 string
	PublicOrigin             *url.URL
	RPGRuntimeOriginTemplate string
	DataDir                  string
	DBPath                   string
	DependencyRoot           string
	DependencyVersions       []string
	ActiveEJSVersion         string
	TrustedProxies           []netip.Prefix
	StartupCheckTimeout      time.Duration
	LogLevel                 string
	MultiDiscImportEnabled   bool
	ServerImportRoots        []ServerImportRoot
	NetplayEnabled           bool
	NetplayMaxActiveRooms    int
	NetplayRoomIdleDraft     time.Duration
	NetplayRoomIdleWaiting   time.Duration
	NetplayReconnectLease    time.Duration
}

type ServerImportRoot struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Path          string `json:"path"`
	CanonicalPath string `json:"-"`
}

type Mode string

const (
	ModeRelease Mode = "release"
	ModeTest    Mode = "test"
)

func ParseMode(value string) (Mode, error) {
	mode := Mode(value)
	if mode != ModeRelease && mode != ModeTest {
		return "", fmt.Errorf("%w: mode", errInvalidConfig)
	}
	return mode, nil
}

type Maintenance struct {
	DataDir            string
	DBPath             string
	DependencyRoot     string
	DependencyVersions []string
	ActiveEJSVersion   string
}

func loadDependencyMaintenance() (Maintenance, error) {
	if err := rejectUnknownVariables(os.Environ()); err != nil {
		return Maintenance{}, err
	}
	dependencyRoot, err := checkedExistingDir("RETROM_DEPENDENCY_ROOT", os.Getenv("RETROM_DEPENDENCY_ROOT"))
	if err != nil {
		return Maintenance{}, err
	}
	versions, err := parseVersions(os.Getenv("RETROM_DEPENDENCY_VERSIONS"))
	if err != nil {
		return Maintenance{}, err
	}
	active := os.Getenv("RETROM_ACTIVE_EMULATORJS_VERSION")
	if !slices.Contains(versions, active) {
		return Maintenance{}, fmt.Errorf("%w: RETROM_ACTIVE_EMULATORJS_VERSION", errInvalidConfig)
	}
	return Maintenance{DependencyRoot: dependencyRoot, DependencyVersions: versions, ActiveEJSVersion: active}, nil
}

func LoadBackupMaintenance() (Maintenance, error) {
	result, err := loadDependencyMaintenance()
	if err != nil {
		return Maintenance{}, err
	}
	result.DataDir, err = checkedDataDir(os.Getenv("RETROM_DATA_DIR"))
	if err != nil {
		return Maintenance{}, err
	}
	result.DBPath = os.Getenv("RETROM_DB_PATH")
	if result.DBPath == "" {
		result.DBPath = filepath.Join(result.DataDir, "retrom.db")
	}
	if !filepath.IsAbs(result.DBPath) || !pathWithin(result.DataDir, result.DBPath) || result.DBPath == result.DataDir {
		return Maintenance{}, fmt.Errorf("%w: RETROM_DB_PATH", errInvalidConfig)
	}
	return result, nil
}

func LoadRestoreMaintenance() (Maintenance, error) {
	return loadDependencyMaintenance()
}

// Independent environment checks stay in one ordered fail-fast startup boundary.
func Load(mode Mode) (Config, error) {
	if mode != ModeRelease && mode != ModeTest {
		return Config{}, fmt.Errorf("%w: mode", errInvalidConfig)
	}
	if err := rejectUnknownVariables(os.Environ()); err != nil {
		return Config{}, err
	}
	base, err := loadBaseConfig()
	if err != nil {
		return Config{}, err
	}
	network, err := loadNetworkConfig(mode)
	if err != nil {
		return Config{}, err
	}
	runtimeOptions, err := loadRuntimeOptions(base.dataDir, base.dependencyRoot)
	if err != nil {
		return Config{}, err
	}
	netplay, err := loadNetplayConfig()
	if err != nil {
		return Config{}, err
	}
	return Config{
		Mode: mode, HTTPAddr: network.httpAddr, PublicOrigin: network.publicOrigin,
		RPGRuntimeOriginTemplate: network.rpgRuntimeOriginTemplate,
		DataDir:                  base.dataDir, DBPath: base.dbPath, DependencyRoot: base.dependencyRoot,
		DependencyVersions: base.versions, ActiveEJSVersion: base.active,
		TrustedProxies: network.proxies, StartupCheckTimeout: runtimeOptions.startupTimeout,
		LogLevel: runtimeOptions.logLevel, MultiDiscImportEnabled: runtimeOptions.multiDiscImportEnabled,
		ServerImportRoots: runtimeOptions.serverImportRoots,
		NetplayEnabled:    netplay.enabled, NetplayMaxActiveRooms: netplay.maxActiveRooms,
		NetplayRoomIdleDraft:   netplay.roomIdleDraft,
		NetplayRoomIdleWaiting: netplay.roomIdleWaiting,
		NetplayReconnectLease:  netplay.reconnectLease,
	}, nil
}

type baseConfig struct {
	dataDir        string
	dependencyRoot string
	versions       []string
	active         string
	dbPath         string
}

func loadBaseConfig() (baseConfig, error) {
	var result baseConfig
	var err error
	result.dataDir, err = checkedDataDir(os.Getenv("RETROM_DATA_DIR"))
	if err != nil {
		return baseConfig{}, err
	}
	result.dependencyRoot, err = checkedExistingDir(
		"RETROM_DEPENDENCY_ROOT", os.Getenv("RETROM_DEPENDENCY_ROOT"),
	)
	if err != nil {
		return baseConfig{}, err
	}
	result.versions, err = parseVersions(os.Getenv("RETROM_DEPENDENCY_VERSIONS"))
	if err != nil {
		return baseConfig{}, err
	}
	result.active = os.Getenv("RETROM_ACTIVE_EMULATORJS_VERSION")
	if !slices.Contains(result.versions, result.active) {
		return baseConfig{}, fmt.Errorf("%w: RETROM_ACTIVE_EMULATORJS_VERSION", errInvalidConfig)
	}
	result.dbPath = os.Getenv("RETROM_DB_PATH")
	if result.dbPath == "" {
		result.dbPath = filepath.Join(result.dataDir, "retrom.db")
	}
	if !filepath.IsAbs(result.dbPath) || !pathWithin(result.dataDir, result.dbPath) ||
		result.dbPath == result.dataDir {
		return baseConfig{}, fmt.Errorf("%w: RETROM_DB_PATH", errInvalidConfig)
	}
	return result, nil
}

type networkConfig struct {
	publicOrigin             *url.URL
	rpgRuntimeOriginTemplate string
	httpAddr                 string
	proxies                  []netip.Prefix
}

func loadNetworkConfig(mode Mode) (networkConfig, error) {
	allowInsecure := os.Getenv("RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN")
	if allowInsecure != "" && allowInsecure != "true" && allowInsecure != "false" {
		return networkConfig{}, fmt.Errorf("%w: RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN", errInvalidConfig)
	}
	publicOrigin, err := parsePublicOrigin(
		os.Getenv("RETROM_PUBLIC_ORIGIN"), mode == ModeTest && allowInsecure == "true",
	)
	if err != nil {
		return networkConfig{}, err
	}
	rpgRuntimeOriginTemplate, err := parseRPGRuntimeOriginTemplate(
		os.Getenv("RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE"), publicOrigin,
		mode == ModeTest && allowInsecure == "true",
	)
	if err != nil {
		return networkConfig{}, err
	}
	httpAddr := os.Getenv("RETROM_HTTP_ADDR")
	if httpAddr == "" {
		return networkConfig{}, fmt.Errorf("%w: RETROM_HTTP_ADDR", errInvalidConfig)
	}
	if _, _, err := net.SplitHostPort(httpAddr); err != nil {
		return networkConfig{}, fmt.Errorf("%w: RETROM_HTTP_ADDR", errInvalidConfig)
	}
	proxies, err := parseTrustedProxies(os.Getenv("RETROM_TRUSTED_PROXIES"))
	if err != nil {
		return networkConfig{}, err
	}
	return networkConfig{
		publicOrigin: publicOrigin, rpgRuntimeOriginTemplate: rpgRuntimeOriginTemplate,
		httpAddr: httpAddr, proxies: proxies,
	}, nil
}

func parseRPGRuntimeOriginTemplate(value string, publicOrigin *url.URL, allowInsecure bool) (string, error) {
	const marker = "00000000-0000-4000-8000-000000000000"
	if !validRPGRuntimeTemplatePrefix(value, allowInsecure) {
		return "", fmt.Errorf("%w: RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE", errInvalidConfig)
	}
	concrete := strings.Replace(value, "{launchId}", marker, 1)
	parsed, err := url.Parse(concrete)
	if err != nil || !validRPGRuntimeTemplateURL(parsed, concrete, marker, publicOrigin, allowInsecure) {
		return "", fmt.Errorf("%w: RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE", errInvalidConfig)
	}
	return value, nil
}

func validRPGRuntimeTemplatePrefix(value string, allowInsecure bool) bool {
	if value == "" || strings.Count(value, "{launchId}") != 1 {
		return false
	}
	return strings.HasPrefix(value, "https://{launchId}.") ||
		allowInsecure && strings.HasPrefix(value, "http://{launchId}.")
}

func validRPGRuntimeTemplateURL(
	parsed *url.URL,
	concrete, marker string,
	publicOrigin *url.URL,
	allowInsecure bool,
) bool {
	if parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.String() != concrete || publicOrigin == nil {
		return false
	}
	hostParts := strings.Split(parsed.Hostname(), ".")
	if len(hostParts) == 0 || hostParts[0] != marker || concrete == publicOrigin.String() {
		return false
	}
	validScheme := parsed.Scheme == "https" || allowInsecure && parsed.Scheme == "http"
	return validScheme && parsed.Scheme == publicOrigin.Scheme
}

type runtimeOptions struct {
	startupTimeout         time.Duration
	logLevel               string
	multiDiscImportEnabled bool
	serverImportRoots      []ServerImportRoot
}

func loadRuntimeOptions(dataDir, dependencyRoot string) (runtimeOptions, error) {
	result := runtimeOptions{startupTimeout: 60 * time.Second, logLevel: "info"}
	var err error
	if value := os.Getenv("RETROM_STARTUP_CHECK_TIMEOUT"); value != "" {
		result.startupTimeout, err = time.ParseDuration(value)
		if err != nil || result.startupTimeout < 10*time.Second || result.startupTimeout > 5*time.Minute {
			return runtimeOptions{}, fmt.Errorf("%w: RETROM_STARTUP_CHECK_TIMEOUT", errInvalidConfig)
		}
	}
	if value := os.Getenv("RETROM_LOG_LEVEL"); value != "" {
		result.logLevel = value
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, result.logLevel) {
		return runtimeOptions{}, fmt.Errorf("%w: RETROM_LOG_LEVEL", errInvalidConfig)
	}
	result.multiDiscImportEnabled, err = parseStrictBoolean(
		"RETROM_MULTI_DISC_IMPORT_ENABLED", os.Getenv("RETROM_MULTI_DISC_IMPORT_ENABLED"), false,
	)
	if err != nil {
		return runtimeOptions{}, err
	}
	result.serverImportRoots, err = parseServerImportRoots(
		os.Getenv("RETROM_SERVER_IMPORT_ROOTS"), dataDir, dependencyRoot,
	)
	if err != nil {
		return runtimeOptions{}, err
	}
	return result, nil
}

type netplayConfig struct {
	enabled         bool
	maxActiveRooms  int
	roomIdleDraft   time.Duration
	roomIdleWaiting time.Duration
	reconnectLease  time.Duration
}

func loadNetplayConfig() (netplayConfig, error) {
	var result netplayConfig
	var err error
	result.enabled, err = parseStrictBoolean(
		"RETROM_NETPLAY_ENABLED", os.Getenv("RETROM_NETPLAY_ENABLED"), false,
	)
	if err != nil {
		return netplayConfig{}, err
	}
	result.maxActiveRooms, err = parseIntegerRange(
		"RETROM_NETPLAY_MAX_ACTIVE_ROOMS", os.Getenv("RETROM_NETPLAY_MAX_ACTIVE_ROOMS"), 16, 1, 128,
	)
	if err != nil {
		return netplayConfig{}, err
	}
	result.roomIdleDraft, err = parseFixedMilliseconds(
		"RETROM_NETPLAY_ROOM_IDLE_DRAFT_MS", os.Getenv("RETROM_NETPLAY_ROOM_IDLE_DRAFT_MS"), 900_000,
	)
	if err != nil {
		return netplayConfig{}, err
	}
	result.roomIdleWaiting, err = parseFixedMilliseconds(
		"RETROM_NETPLAY_ROOM_IDLE_WAITING_MS", os.Getenv("RETROM_NETPLAY_ROOM_IDLE_WAITING_MS"), 1_800_000,
	)
	if err != nil {
		return netplayConfig{}, err
	}
	result.reconnectLease, err = parseFixedMilliseconds(
		"RETROM_NETPLAY_RECONNECT_LEASE_MS", os.Getenv("RETROM_NETPLAY_RECONNECT_LEASE_MS"), 10_000,
	)
	if err != nil {
		return netplayConfig{}, err
	}
	return result, nil
}

func parseIntegerRange(name, raw string, defaultValue, minimum, maximum int) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum || strconv.Itoa(value) != raw {
		return 0, fmt.Errorf("%w: %s", errInvalidConfig, name)
	}
	return value, nil
}

func parseFixedMilliseconds(name, raw string, expected int) (time.Duration, error) {
	if raw == "" {
		return time.Duration(expected) * time.Millisecond, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value != expected || strconv.Itoa(value) != raw {
		return 0, fmt.Errorf("%w: %s", errInvalidConfig, name)
	}
	return time.Duration(value) * time.Millisecond, nil
}

// Each branch enforces an independent closed-schema or filesystem boundary invariant.
func parseServerImportRoots(raw, dataDir, dependencyRoot string) ([]ServerImportRoot, error) {
	if raw == "" {
		return []ServerImportRoot{}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var roots []ServerImportRoot
	if err := decoder.Decode(&roots); err != nil || roots == nil || decoder.More() || len(roots) > 8 {
		return nil, fmt.Errorf("%w: RETROM_SERVER_IMPORT_ROOTS", errInvalidConfig)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: RETROM_SERVER_IMPORT_ROOTS", errInvalidConfig)
	}
	ids := make(map[string]struct{}, len(roots))
	labels := make(map[string]struct{}, len(roots))
	home, _ := os.UserHomeDir()
	for index := range roots {
		if err := validateServerImportRoot(
			&roots[index], roots[:index], ids, labels, home, dataDir, dependencyRoot,
		); err != nil {
			return nil, err
		}
	}
	return roots, nil
}

func validateServerImportRoot(
	root *ServerImportRoot,
	previous []ServerImportRoot,
	ids, labels map[string]struct{},
	home, dataDir, dependencyRoot string,
) error {
	_, duplicateID := ids[root.ID]
	_, duplicateLabel := labels[root.Label]
	if !validServerImportRootShape(*root) || duplicateID || duplicateLabel {
		return fmt.Errorf("%w: RETROM_SERVER_IMPORT_ROOTS", errInvalidConfig)
	}
	canonical, err := canonicalDirectoryWithoutSymlinks(root.Path)
	if err != nil || !validCanonicalImportRoot(canonical, home, dataDir, dependencyRoot) {
		return fmt.Errorf("%w: RETROM_SERVER_IMPORT_ROOTS", errInvalidConfig)
	}
	for _, configured := range previous {
		if pathsOverlap(canonical, configured.CanonicalPath) {
			return fmt.Errorf("%w: RETROM_SERVER_IMPORT_ROOTS", errInvalidConfig)
		}
	}
	ids[root.ID], labels[root.Label] = struct{}{}, struct{}{}
	root.CanonicalPath = canonical
	return nil
}

func validServerImportRootShape(root ServerImportRoot) bool {
	validLabel := root.Label == strings.TrimSpace(root.Label) && len([]rune(root.Label)) >= 1 &&
		len([]rune(root.Label)) <= 40 && len([]byte(root.Label)) <= 160 && !containsControl(root.Label)
	validPath := root.Path != "" && filepath.IsAbs(root.Path) && filepath.Clean(root.Path) == root.Path
	return validServerImportRootID(root.ID) && validLabel && validPath
}

func validCanonicalImportRoot(canonical, home, dataDir, dependencyRoot string) bool {
	return canonical != string(filepath.Separator) && canonical != home &&
		!pathsOverlap(canonical, dataDir) && !pathsOverlap(canonical, dependencyRoot)
}

func validServerImportRootID(value string) bool {
	if len(value) < 1 || len(value) > 32 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func containsControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character >= 0x7f && character <= 0x9f {
			return true
		}
	}
	return false
}

func canonicalDirectoryWithoutSymlinks(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errInvalidConfig
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return "", errInvalidConfig
	}
	return canonical, nil
}

func pathsOverlap(left, right string) bool {
	return pathWithin(left, right) || pathWithin(right, left)
}

func parseStrictBoolean(name, value string, defaultValue bool) (bool, error) {
	if value == "" {
		return defaultValue, nil
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%w: %s", errInvalidConfig, name)
	}
}

func rejectUnknownVariables(environment []string) error {
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if !found || !strings.HasPrefix(name, "RETROM_") {
			continue
		}
		if _, ok := knownVariables[name]; ok {
			continue
		}
		ignored := false
		for _, prefix := range ignoredPrefixes {
			if strings.HasPrefix(name, prefix) {
				ignored = true
				break
			}
		}
		if !ignored {
			return fmt.Errorf("%w: %s", errUnknownVariable, name)
		}
	}
	return nil
}

func checkedDataDir(raw string) (string, error) {
	if raw == "" || !filepath.IsAbs(raw) || filepath.Clean(raw) != raw {
		return "", fmt.Errorf("%w: RETROM_DATA_DIR", errInvalidConfig)
	}
	home, _ := os.UserHomeDir()
	if raw == string(filepath.Separator) || raw == home {
		return "", fmt.Errorf("%w: RETROM_DATA_DIR", errInvalidConfig)
	}
	// raw is already absolute, clean, non-root and different from the current user's home directory.
	if info, err := os.Lstat(raw); err == nil &&
		info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: RETROM_DATA_DIR", errInvalidConfig)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: RETROM_DATA_DIR", errInvalidConfig)
	}
	if err := os.MkdirAll(raw, 0o700); err != nil {
		return "", fmt.Errorf("%w: RETROM_DATA_DIR", errInvalidConfig)
	}
	return raw, nil
}

func checkedExistingDir(name, raw string) (string, error) {
	if raw == "" || !filepath.IsAbs(raw) || filepath.Clean(raw) != raw {
		return "", fmt.Errorf("%w: %s", errInvalidConfig, name)
	}
	home, _ := os.UserHomeDir()
	if raw == string(filepath.Separator) || raw == home {
		return "", fmt.Errorf("%w: %s", errInvalidConfig, name)
	}
	// raw passed the same absolute/clean/non-root boundary above and must already exist.
	info, err := os.Lstat(raw)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %s", errInvalidConfig, name)
	}
	return raw, nil
}

func parseVersions(raw string) ([]string, error) {
	values := strings.Split(raw, ",")
	if raw == "" || len(values) == 0 {
		return nil, fmt.Errorf("%w: RETROM_DEPENDENCY_VERSIONS", errInvalidConfig)
	}
	seen := make(map[string]struct{}, len(values))
	var previous parsedSemver
	for _, value := range values {
		current, err := parseSemver(value)
		if err != nil || strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("%w: RETROM_DEPENDENCY_VERSIONS", errInvalidConfig)
		}
		if _, ok := seen[value]; ok || len(seen) > 0 && compareSemver(current, previous) <= 0 {
			return nil, fmt.Errorf("%w: RETROM_DEPENDENCY_VERSIONS", errInvalidConfig)
		}
		seen[value] = struct{}{}
		previous = current
	}
	return values, nil
}

type parsedSemver struct {
	numbers    [3]int
	prerelease string
}

func parseSemver(value string) (parsedSemver, error) {
	version, prerelease, hasPrerelease := strings.Cut(value, "-")
	parts := strings.Split(version, ".")
	if value == "" || len(parts) != 3 || strings.Contains(prerelease, "+") ||
		hasPrerelease && !validPrerelease(prerelease) {
		return parsedSemver{}, errInvalidConfig
	}
	result := parsedSemver{prerelease: prerelease}
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || len(part) > 1 && part[0] == '0' {
			return parsedSemver{}, errInvalidConfig
		}
		result.numbers[index] = number
	}
	return result, nil
}

func validPrerelease(value string) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if !validPrereleaseIdentifier(identifier) {
			return false
		}
	}
	return true
}

func validPrereleaseIdentifier(identifier string) bool {
	if identifier == "" {
		return false
	}
	numeric := true
	for _, character := range identifier {
		if character < '0' || character > '9' {
			numeric = false
		}
		if character != '-' && (character < '0' || character > '9') &&
			(character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return !numeric || len(identifier) == 1 || identifier[0] != '0'
}

func compareSemver(left, right parsedSemver) int {
	for index := range left.numbers {
		if left.numbers[index] < right.numbers[index] {
			return -1
		}
		if left.numbers[index] > right.numbers[index] {
			return 1
		}
	}
	if left.prerelease == right.prerelease {
		return 0
	}
	if left.prerelease == "" {
		return 1
	}
	if right.prerelease == "" {
		return -1
	}
	return comparePrerelease(left.prerelease, right.prerelease)
}

func comparePrerelease(left, right string) int {
	leftParts, rightParts := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < min(len(leftParts), len(rightParts)); index++ {
		leftNumber, leftErr := strconv.Atoi(leftParts[index])
		rightNumber, rightErr := strconv.Atoi(rightParts[index])
		switch {
		case leftErr == nil && rightErr == nil && leftNumber != rightNumber:
			if leftNumber < rightNumber {
				return -1
			}
			return 1
		case leftErr == nil && rightErr != nil:
			return -1
		case leftErr != nil && rightErr == nil:
			return 1
		case leftParts[index] != rightParts[index]:
			if leftParts[index] < rightParts[index] {
				return -1
			}
			return 1
		}
	}
	if len(leftParts) < len(rightParts) {
		return -1
	}
	return 1
}

func parsePublicOrigin(raw string, allowInsecure bool) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: RETROM_PUBLIC_ORIGIN", errInvalidConfig)
	}
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || (!allowInsecure && parsed.Hostname() != "localhost")) {
		return nil, fmt.Errorf("%w: RETROM_PUBLIC_ORIGIN", errInvalidConfig)
	}
	return parsed, nil
}

func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	if raw == "" {
		return nil, nil
	}
	values := strings.Split(raw, ",")
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() == 0 || value != strings.TrimSpace(value) || value != prefix.Masked().String() {
			return nil, fmt.Errorf("%w: RETROM_TRUSTED_PROXIES", errInvalidConfig)
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
