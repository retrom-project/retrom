package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/testassert"
)

func TestParsePublicOriginRequiresExplicitDevelopmentOptInForHTTPHosts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		value         string
		allowInsecure bool
		wantErr       bool
	}{
		{name: "https domain", value: "https://retrom.example"},
		{name: "localhost http", value: "http://localhost:3000"},
		{name: "PFB host rejected by default", value: "http://feature-a1b2c3d4e5f6.localhost:3000", wantErr: true},
		{name: "PFB host with opt in", value: "http://feature-a1b2c3d4e5f6.localhost:3000", allowInsecure: true},
		{name: "local acceptance site with opt in", value: "http://retrom-app.rpg.localhost:13004", allowInsecure: true},
		{name: "arbitrary HTTP host remains invalid", value: "http://dev.example:3000", allowInsecure: true, wantErr: true},
		{name: "credentials remain invalid", value: "http://user@localhost:3000", allowInsecure: true, wantErr: true},
		{name: "path remains invalid", value: "http://localhost:3000/path", allowInsecure: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePublicOrigin(test.value, test.allowInsecure)
			testassert.Falsef(t, (err != nil) != test.wantErr, "parsePublicOrigin(%q, %t) error = %v", test.value, test.allowInsecure, err)
		})
	}
}

func TestParseRPGRuntimeOriginTemplateRequiresUniqueLaunchLabelAndMatchingScheme(t *testing.T) {
	t.Parallel()
	httpsOrigin, err := parsePublicOrigin("https://retrom.example", false)
	if err != nil {
		t.Fatal(err)
	}
	httpOrigin, err := parsePublicOrigin("http://feature-a1b2c3d4e5f6.localhost:3000", true)
	if err != nil {
		t.Fatal(err)
	}
	localhostOrigin, err := parsePublicOrigin("http://localhost:13004", true)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		value         string
		origin        *url.URL
		allowInsecure bool
		wantErr       bool
	}{
		{name: "release", value: "https://{launchId}.runtime.retrom.example", origin: httpsOrigin},
		{name: "PFB", value: "http://{launchId}.feature-a1b2c3d4e5f6.rpg.localhost:3000", origin: httpOrigin, allowInsecure: true},
		{name: "localhost test", value: "http://{launchId}.rpg.localhost:18084", origin: localhostOrigin, allowInsecure: true},
		{name: "wrong PFB", value: "http://{launchId}.feature-fedcba987654.rpg.localhost:3000", origin: httpOrigin, allowInsecure: true, wantErr: true},
		{name: "mixed content", value: "http://{launchId}.rpg.localhost:8080", origin: httpsOrigin, allowInsecure: true, wantErr: true},
		{name: "placeholder not leftmost", value: "https://rpg.{launchId}.retrom.example", origin: httpsOrigin, wantErr: true},
		{name: "placeholder path", value: "https://runtime.retrom.example/{launchId}", origin: httpsOrigin, wantErr: true},
		{name: "duplicate placeholder", value: "https://{launchId}.{launchId}.retrom.example", origin: httpsOrigin, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, parseErr := parseRPGRuntimeOriginTemplate(test.value, test.origin, test.allowInsecure)
			testassert.Falsef(t, (parseErr != nil) != test.wantErr, "parseRPGRuntimeOriginTemplate(%q) error = %v", test.value, parseErr)
		})
	}
}

func TestPFBCandidateBoundaryRequiresTestModeAndMatchingLocalOrigin(t *testing.T) {
	dependencyRoot := t.TempDir()
	runtimeRoot := filepath.Join(dependencyRoot, "runtime", "rpgmaker", "v1")
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	identifier := "feature-a1b2c3d4e5f6"
	marker := `{"schemaVersion":1,"kind":"RETROM_PFB_CANDIDATE_V1","pfbId":"` + identifier + `","formalManifestSha256":"` + strings.Repeat("a", 64) + `","runtime":{},"cores":[],"runtimeFiles":[],"artifacts":[],"filesSha256":"` + strings.Repeat("b", 64) + `","overlaySha256":"` + strings.Repeat("c", 64) + `"}`
	if err := os.WriteFile(filepath.Join(runtimeRoot, ".retrom-pfb-candidate.json"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	origin, err := parsePublicOrigin("http://"+identifier+".localhost:3000", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RETROM_PFB_ID", identifier)
	if err := validatePFBBoundary(ModeTest, dependencyRoot, origin); err != nil {
		t.Fatalf("valid PFB boundary rejected: %v", err)
	}
	if err := validatePFBBoundary(ModeRelease, dependencyRoot, origin); err == nil {
		t.Fatal("release mode accepted PFB candidate")
	}
	t.Setenv("RETROM_PFB_ID", "different-aaaaaaaaaaaa")
	if err := validatePFBBoundary(ModeTest, dependencyRoot, origin); err == nil {
		t.Fatal("mismatched PFB ID accepted")
	}
}

func TestParseServerImportRootsStrictSchemaAndOverlap(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	dataDir := filepath.Join(base, "data")
	dependencyRoot := filepath.Join(base, "deps")
	rootA := filepath.Join(base, "source-a")
	rootB := filepath.Join(base, "source-b")
	for _, path := range []string{dataDir, dependencyRoot, rootA, rootB} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	valid := `[{"id":"rom-library","label":"ROM 仓库","path":"` + rootA + `"},{"id":"archive","label":"归档盘","path":"` + rootB + `"}]`
	roots, err := parseServerImportRoots(valid, dataDir, dependencyRoot)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(roots) != 2 }, func() bool { return roots[0].CanonicalPath != rootA }), "valid roots = %#v, %v", roots, err)
	invalid := []string{
		`null`,
		`[] {}`,
		`[{"id":"a","label":"A","path":"` + rootA + `"}] trailing`,
		`[{"id":"Bad","label":"bad","path":"` + rootA + `"}]`,
		`[{"id":"a","label":"A\u0000B","path":"` + rootA + `"}]`,
		`[{"id":"a","label":" same ","path":"` + rootA + `"}]`,
		`[{"id":"a","label":"same","path":"` + rootA + `","extra":true}]`,
		`[{"id":"a","label":"same","path":"` + dataDir + `"}]`,
		`[{"id":"a","label":"same","path":"/"}]`,
		`[{"id":"a","label":"same","path":"relative"}]`,
		`[{"id":"a","label":"same","path":"` + rootA + `"},{"id":"a","label":"other","path":"` + rootB + `"}]`,
		`[{"id":"a","label":"same","path":"` + rootA + `"},{"id":"b","label":"same","path":"` + rootB + `"}]`,
	}
	for _, value := range invalid {
		if _, parseErr := parseServerImportRoots(value, dataDir, dependencyRoot); parseErr == nil {
			t.Errorf("invalid roots accepted: %s", value)
		}
	}
	nested := filepath.Join(rootA, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	overlap := `[{"id":"a","label":"A","path":"` + rootA + `"},{"id":"b","label":"B","path":"` + nested + `"}]`
	if _, err := parseServerImportRoots(overlap, dataDir, dependencyRoot); err == nil {
		t.Fatal("overlapping roots accepted")
	}
	symlink := filepath.Join(base, "source-link")
	if err := os.Symlink(rootA, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := parseServerImportRoots(`[{"id":"link","label":"Link","path":"`+symlink+`"}]`, dataDir, dependencyRoot); err == nil {
		t.Fatal("symlink root accepted")
	}
	filePath := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseServerImportRoots(`[{"id":"file","label":"File","path":"`+filePath+`"}]`, dataDir, dependencyRoot); err == nil {
		t.Fatal("regular file root accepted")
	}
	entries := make([]string, 0, 9)
	for index := range 9 {
		path := filepath.Join(base, fmt.Sprintf("root-%d", index))
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, fmt.Sprintf(`{"id":"r%d","label":"Root %d","path":%q}`, index, index, path))
	}
	if _, err := parseServerImportRoots("["+strings.Join(entries, ",")+"]", dataDir, dependencyRoot); err == nil {
		t.Fatal("nine roots accepted")
	}
}

func TestParseVersionsRequiresStrictSortedSemver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid", value: "4.2.3,4.3.0-pre,5.0.0"},
		{name: "valid prerelease order", value: "4.3.0-pre.2,4.3.0-pre.10,4.3.0"},
		{name: "empty", value: "", wantErr: true},
		{name: "duplicate", value: "4.2.3,4.2.3", wantErr: true},
		{name: "unsorted", value: "5.0.0,4.2.3", wantErr: true},
		{name: "whitespace", value: "4.2.3, 5.0.0", wantErr: true},
		{name: "leading zero", value: "04.2.3", wantErr: true},
		{name: "prerelease leading zero", value: "4.3.0-pre.01", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseVersions(test.value)
			testassert.Falsef(t, (err != nil) != test.wantErr, "parseVersions(%q) error = %v", test.value, err)
		})
	}
}

func TestRejectUnknownVariablesAllowsToolPrefixesOnly(t *testing.T) {
	t.Parallel()
	if err := rejectUnknownVariables([]string{"RETROM_ACCEPTANCE_BASE_URL=https://example.invalid", "RETROM_HTTP_ADDR=x", "RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN=true", "RETROM_MULTI_DISC_IMPORT_ENABLED=false", "RETROM_NETPLAY_ENABLED=true"}); err != nil {
		t.Fatalf("known variables rejected: %v", err)
	}
	for _, variable := range []string{"RETROM_DATA_DI=typo", "RETROM_EXAMPLE_ROOT=removed"} {
		if err := rejectUnknownVariables([]string{variable}); err == nil {
			t.Fatalf("unknown RETROM variable %q was accepted", variable)
		}
	}
}

func TestParseNetplayCapacityAndFixedProtocolTimers(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"1", "16", "128"} {
		if _, err := parseIntegerRange("RETROM_NETPLAY_MAX_ACTIVE_ROOMS", value, 16, 1, 128); err != nil {
			t.Fatalf("valid room capacity %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"0", "129", "01", " 16"} {
		if _, err := parseIntegerRange("RETROM_NETPLAY_MAX_ACTIVE_ROOMS", value, 16, 1, 128); err == nil {
			t.Fatalf("invalid room capacity %q accepted", value)
		}
	}
	if duration, err := parseFixedMilliseconds("RETROM_NETPLAY_RECONNECT_LEASE_MS", "", 10_000); err != nil || duration != 10_000_000_000 {
		t.Fatalf("default reconnect lease = %s, %v", duration, err)
	}
	for _, value := range []string{"9999", "10001", "010000"} {
		if _, err := parseFixedMilliseconds("RETROM_NETPLAY_RECONNECT_LEASE_MS", value, 10_000); err == nil {
			t.Fatalf("invalid reconnect lease %q accepted", value)
		}
	}
}

func TestParseStrictBooleanRejectsImplicitOrMisspelledValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value        string
		defaultValue bool
		want         bool
		wantErr      bool
	}{
		{value: "", want: false},
		{value: "", defaultValue: true, want: true},
		{value: "true", want: true},
		{value: "false", defaultValue: true, want: false},
		{value: "TRUE", wantErr: true},
		{value: "1", wantErr: true},
		{value: " true", wantErr: true},
	}
	for _, test := range tests {
		result, err := parseStrictBoolean("RETROM_MULTI_DISC_IMPORT_ENABLED", test.value, test.defaultValue)
		testassert.CheckFalsef(t, testassert.Any(func() bool { return (err != nil) != test.wantErr }, func() bool { return !test.wantErr && result != test.want }), "parseStrictBoolean(%q, %t) = %t, %v", test.value, test.defaultValue, result, err)
	}
}

func TestParseModeIsClosed(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"release", "test"} {
		if mode, err := ParseMode(value); err != nil || string(mode) != value {
			t.Fatalf("ParseMode(%q) = %q, %v", value, mode, err)
		}
	}
	for _, value := range []string{"", "Test", "development"} {
		if _, err := ParseMode(value); err == nil {
			t.Fatalf("ParseMode(%q) succeeded", value)
		}
	}
}

func TestParseTrustedProxiesRequiresCanonicalCIDRs(t *testing.T) {
	t.Parallel()
	valid, err := parseTrustedProxies("10.0.0.0/8,2001:db8::/32")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(valid) != 2 }), "valid trusted proxies = %#v, %v", valid, err)
	for _, value := range []string{
		"0.0.0.0/0", "10.0.0.1/8", "10.0.0.0/8, 192.0.2.0/24", "not-a-prefix",
	} {
		if _, err := parseTrustedProxies(value); err == nil {
			t.Fatalf("non-canonical trusted proxy %q accepted", value)
		}
	}
}
