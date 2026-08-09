package config

import (
	"testing"
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
		{name: "development domain rejected by default", value: "http://local.sendev.cc:3000", wantErr: true},
		{name: "development domain with opt in", value: "http://local.sendev.cc:3000", allowInsecure: true},
		{name: "credentials remain invalid", value: "http://user@local.sendev.cc:3000", allowInsecure: true, wantErr: true},
		{name: "path remains invalid", value: "http://local.sendev.cc:3000/path", allowInsecure: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePublicOrigin(test.value, test.allowInsecure)
			if (err != nil) != test.wantErr {
				t.Fatalf("parsePublicOrigin(%q, %t) error = %v", test.value, test.allowInsecure, err)
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
			if (err != nil) != test.wantErr {
				t.Fatalf("parseVersions(%q) error = %v", test.value, err)
			}
		})
	}
}

func TestRejectUnknownVariablesAllowsToolPrefixesOnly(t *testing.T) {
	t.Parallel()
	if err := rejectUnknownVariables([]string{"RETROM_FIXTURE_ROOT=secret", "RETROM_HTTP_ADDR=x", "RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN=true"}); err != nil {
		t.Fatalf("known variables rejected: %v", err)
	}
	if err := rejectUnknownVariables([]string{"RETROM_DATA_DI=typo"}); err == nil {
		t.Fatal("unknown RETROM variable was accepted")
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
	if err != nil || len(valid) != 2 {
		t.Fatalf("valid trusted proxies = %#v, %v", valid, err)
	}
	for _, value := range []string{
		"0.0.0.0/0", "10.0.0.1/8", "10.0.0.0/8, 192.0.2.0/24", "not-a-prefix",
	} {
		if _, err := parseTrustedProxies(value); err == nil {
			t.Fatalf("non-canonical trusted proxy %q accepted", value)
		}
	}
}
