package config

import (
	"errors"
	"fmt"
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
	"RETROM_DB_PATH":                      {}, "RETROM_DEPENDENCY_ROOT": {}, "RETROM_DEPENDENCY_VERSIONS": {},
	"RETROM_ACTIVE_EMULATORJS_VERSION": {}, "RETROM_TRUSTED_PROXIES": {},
	"RETROM_STARTUP_CHECK_TIMEOUT": {}, "RETROM_LOG_LEVEL": {},
}

var ignoredPrefixes = []string{
	"RETROM_ACCEPTANCE_", "RETROM_CHROME_", "RETROM_EJS_DEP_", "RETROM_EXAMPLE_",
	"RETROM_FIXTURE_", "RETROM_SMOKE_",
}

type Config struct {
	HTTPAddr            string
	PublicOrigin        *url.URL
	DataDir             string
	DBPath              string
	DependencyRoot      string
	DependencyVersions  []string
	ActiveEJSVersion    string
	TrustedProxies      []netip.Prefix
	StartupCheckTimeout time.Duration
	LogLevel            string
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

//nolint:gocyclo // Environment branches are independent fail-fast contract checks in a single configuration source.
func Load() (Config, error) {
	if err := rejectUnknownVariables(os.Environ()); err != nil {
		return Config{}, err
	}

	dataDir, err := checkedDataDir(os.Getenv("RETROM_DATA_DIR"))
	if err != nil {
		return Config{}, err
	}
	dependencyRoot, err := checkedExistingDir("RETROM_DEPENDENCY_ROOT", os.Getenv("RETROM_DEPENDENCY_ROOT"))
	if err != nil {
		return Config{}, err
	}
	versions, err := parseVersions(os.Getenv("RETROM_DEPENDENCY_VERSIONS"))
	if err != nil {
		return Config{}, err
	}
	active := os.Getenv("RETROM_ACTIVE_EMULATORJS_VERSION")
	if !slices.Contains(versions, active) {
		return Config{}, fmt.Errorf("%w: RETROM_ACTIVE_EMULATORJS_VERSION", errInvalidConfig)
	}
	allowInsecurePublicOrigin := os.Getenv("RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN")
	if allowInsecurePublicOrigin != "" && allowInsecurePublicOrigin != "true" && allowInsecurePublicOrigin != "false" {
		return Config{}, fmt.Errorf("%w: RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN", errInvalidConfig)
	}
	publicOrigin, err := parsePublicOrigin(
		os.Getenv("RETROM_PUBLIC_ORIGIN"),
		allowInsecurePublicOrigin == "true",
	)
	if err != nil {
		return Config{}, err
	}
	httpAddr := os.Getenv("RETROM_HTTP_ADDR")
	if httpAddr == "" {
		return Config{}, fmt.Errorf("%w: RETROM_HTTP_ADDR", errInvalidConfig)
	}
	if _, _, splitErr := net.SplitHostPort(httpAddr); splitErr != nil {
		return Config{}, fmt.Errorf("%w: RETROM_HTTP_ADDR", errInvalidConfig)
	}
	dbPath := os.Getenv("RETROM_DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "retrom.db")
	}
	if !filepath.IsAbs(dbPath) || !pathWithin(dataDir, dbPath) || dbPath == dataDir {
		return Config{}, fmt.Errorf("%w: RETROM_DB_PATH", errInvalidConfig)
	}
	proxies, err := parseTrustedProxies(os.Getenv("RETROM_TRUSTED_PROXIES"))
	if err != nil {
		return Config{}, err
	}
	startupTimeout := 60 * time.Second
	if value := os.Getenv("RETROM_STARTUP_CHECK_TIMEOUT"); value != "" {
		startupTimeout, err = time.ParseDuration(value)
		if err != nil || startupTimeout < 10*time.Second || startupTimeout > 5*time.Minute {
			return Config{}, fmt.Errorf("%w: RETROM_STARTUP_CHECK_TIMEOUT", errInvalidConfig)
		}
	}
	logLevel := os.Getenv("RETROM_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	if !slices.Contains([]string{"debug", "info", "warn", "error"}, logLevel) {
		return Config{}, fmt.Errorf("%w: RETROM_LOG_LEVEL", errInvalidConfig)
	}
	return Config{
		HTTPAddr: httpAddr, PublicOrigin: publicOrigin, DataDir: dataDir, DBPath: dbPath,
		DependencyRoot: dependencyRoot, DependencyVersions: versions, ActiveEJSVersion: active,
		TrustedProxies: proxies, StartupCheckTimeout: startupTimeout, LogLevel: logLevel,
	}, nil
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
	if info, err := os.Lstat(raw); err == nil && //nolint:gosec // Validated non-root configuration path.
		info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: RETROM_DATA_DIR", errInvalidConfig)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: RETROM_DATA_DIR", errInvalidConfig)
	}
	if err := os.MkdirAll(raw, 0o700); err != nil { //nolint:gosec // Validated configuration root.
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
	info, err := os.Lstat(raw) //nolint:gosec // Validated dependency root.
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
	previous := [3]int{-1, -1, -1}
	for _, value := range values {
		parts := strings.Split(value, ".")
		if value == "" || strings.TrimSpace(value) != value || len(parts) != 3 {
			return nil, fmt.Errorf("%w: RETROM_DEPENDENCY_VERSIONS", errInvalidConfig)
		}
		current := [3]int{}
		for index, part := range parts {
			number, err := strconv.Atoi(part)
			if err != nil || number < 0 || (len(part) > 1 && part[0] == '0') {
				return nil, fmt.Errorf("%w: RETROM_DEPENDENCY_VERSIONS", errInvalidConfig)
			}
			current[index] = number
		}
		if _, ok := seen[value]; ok || compareSemver(current, previous) <= 0 {
			return nil, fmt.Errorf("%w: RETROM_DEPENDENCY_VERSIONS", errInvalidConfig)
		}
		seen[value] = struct{}{}
		previous = current
	}
	return values, nil
}

func compareSemver(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
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
		if err != nil || prefix.Bits() == 0 || value != strings.TrimSpace(value) {
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
