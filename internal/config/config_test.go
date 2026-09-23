package config

import (
	"net/url"
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
		{name: "PFB", value: "http://{launchId}.rpg.feature-a1b2c3d4e5f6.localhost:3000", origin: httpOrigin, allowInsecure: true},
		{name: "localhost test", value: "http://{launchId}.rpg.localhost:18084", origin: localhostOrigin, allowInsecure: true},
		{name: "wrong PFB", value: "http://{launchId}.rpg.feature-fedcba987654.localhost:3000", origin: httpOrigin, allowInsecure: true, wantErr: true},
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
	identifier := "feature-a1b2c3d4e5f6"
	origin, err := parsePublicOrigin("http://"+identifier+".localhost:3000", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RETROM_PFB_ID", identifier)
	pfbID, err := validatePFBBoundary(ModeTest, origin)
	if err != nil {
		t.Fatalf("valid PFB boundary rejected: %v", err)
	}
	if pfbID != identifier {
		t.Fatalf("PFB ID = %q, want %q", pfbID, identifier)
	}
	if _, err := validatePFBBoundary(ModeRelease, origin); err == nil {
		t.Fatal("release mode accepted PFB candidate")
	}
	t.Setenv("RETROM_PFB_ID", "different-aaaaaaaaaaaa")
	if _, err := validatePFBBoundary(ModeTest, origin); err == nil {
		t.Fatal("mismatched PFB ID accepted")
	}
	t.Setenv("RETROM_PFB_ID", "")
	if pfbID, err := validatePFBBoundary(ModeRelease, origin); err != nil || pfbID != "" {
		t.Fatalf("non-PFB release boundary = %q, %v", pfbID, err)
	}
}

func TestPFBProviderDevBoundaryFailsClosedOutsideMatchingTestPFB(t *testing.T) {
	root := t.TempDir()
	identifier := "feature-a1b2c3d4e5f6"
	if value, err := validateProviderDevBoundary(ModeTest, identifier, root); err != nil || value != root {
		t.Fatalf("matching test PFB dev root = %q, %v", value, err)
	}
	for _, test := range []struct {
		name string
		mode Mode
		pfb  string
		root string
	}{
		{name: "release", mode: ModeRelease, pfb: identifier, root: root},
		{name: "missing PFB id", mode: ModeTest, root: root},
		{name: "missing dev root", mode: ModeTest, pfb: identifier},
		{name: "relative dev root", mode: ModeTest, pfb: identifier, root: "relative"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateProviderDevBoundary(test.mode, test.pfb, test.root); err == nil {
				t.Fatal("loose provider boundary accepted invalid configuration")
			}
		})
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
	if err := rejectUnknownVariables([]string{"RETROM_ACCEPTANCE_BASE_URL=https://example.invalid", "RETROM_HTTP_ADDR=x", "RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN=true", "RETROM_MULTI_DISC_IMPORT_ENABLED=false"}); err != nil {
		t.Fatalf("known variables rejected: %v", err)
	}
	for _, variable := range []string{"RETROM_DATA_DI=typo", "RETROM_EXAMPLE_ROOT=removed", "RETROM_TRUSTED_PROXIES=10.0.0.0/8"} {
		if err := rejectUnknownVariables([]string{variable}); err == nil {
			t.Fatalf("unknown RETROM variable %q was accepted", variable)
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
