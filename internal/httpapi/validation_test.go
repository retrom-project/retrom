package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestClientIPTrustsInternalProxiesByDefault(t *testing.T) {
	t.Parallel()
	s := &Server{}
	cases := []struct{ name, remote, forwarded, want string }{
		{"public peer", "192.0.2.2:9000", "198.51.100.1", "192.0.2.2"},
		{"docker proxy", "172.16.16.18:9000", "198.51.100.1", "198.51.100.1"},
		{"private 10 proxy", "10.0.0.2:9000", "198.51.100.1", "198.51.100.1"},
		{"private 192 proxy", "192.168.1.2:9000", "198.51.100.1", "198.51.100.1"},
		{"loopback proxy", "127.0.0.1:9000", "198.51.100.1", "198.51.100.1"},
		{"IPv6 loopback", "[::1]:9000", "2001:db8::1", "2001:db8::1"},
		{"IPv6 private proxy", "[fd12::2]:9000", "2001:db8::1", "2001:db8::1"},
		{"IPv6 public peer", "[2001:db8::2]:9000", "198.51.100.1", "2001:db8::2"},
		{"mapped private proxy", "[::ffff:172.16.16.18]:9000", "198.51.100.1", "198.51.100.1"},
		{"internal chain", "172.29.240.2:9000", "198.51.100.1, 172.29.240.3", "198.51.100.1"},
		{"untrusted hop", "172.29.240.2:9000", "203.0.113.1, 192.0.2.5", "192.0.2.5"},
		{"LAN client", "172.16.16.18:9000", "192.168.1.50, 10.0.0.2", "192.168.1.50"},
		{"malformed hop", "172.29.240.2:9000", "198.51.100.1, garbage", "172.29.240.2"},
		{"missing header", "172.29.240.2:9000", "", "172.29.240.2"},
		{"too many hops", "172.29.240.2:9000", strings.Repeat("10.0.0.1,", 16) + "198.51.100.1", "172.29.240.2"},
		{"malformed peer", "invalid", "198.51.100.1", "unknown"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
			r.RemoteAddr = item.remote
			r.Header.Set("X-Forwarded-For", item.forwarded)
			if value := s.clientIP(r); value != item.want {
				t.Fatalf("IP=%s want=%s", value, item.want)
			}
		})
	}
}

func TestClientIPRejectsMultipleForwardedHeaders(t *testing.T) {
	t.Parallel()
	s := &Server{}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
	r.RemoteAddr = "172.16.16.18:9000"
	r.Header.Add("X-Forwarded-For", "198.51.100.1")
	r.Header.Add("X-Forwarded-For", "203.0.113.1")
	if value := s.clientIP(r); value != "172.16.16.18" {
		t.Fatalf("multiple forwarded headers accepted: %s", value)
	}
}

func TestOriginProtectionRequiresExactSingleOrigin(t *testing.T) {
	t.Parallel()
	s := &Server{Origin: "https://retrom.example"}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
	for _, origin := range []string{"", "https://retrom.example.evil", "null", "https://retrom.example:443"} {
		r.Header.Set("Origin", origin)
		if s.originValid(r) {
			t.Fatalf("accepted %q", origin)
		}
	}
	r.Header.Set("Origin", s.Origin)
	if !s.originValid(r) {
		t.Fatal("exact origin rejected")
	}
	r.Header.Add("Origin", s.Origin)
	if s.originValid(r) {
		t.Fatal("multiple origin headers accepted")
	}
}

func TestStrictJSONRejectsDuplicateAndTrailingInput(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`{"name":"a","name":"b"}`, `{"name":"a"} {}`, `{"name":"a","other":1}`} {
		var output struct {
			Name string `json:"name"`
		}
		if err := strictJSON(strings.NewReader(value), &output); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("accepted %s: %v", value, err)
		}
	}
}

func TestContractValidatesEncodedBiosRequirementSegment(t *testing.T) {
	t.Parallel()
	validate, err := contractValidator()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "http://retrom.example/api/v1/admin/bios/firmware%2Fgba_bios.bin", strings.NewReader(""))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=example")
	if err = validate(r); err != nil {
		t.Fatalf("encoded requirement key rejected: %v", err)
	}
	bad := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://retrom.example/api/v1/games/not-a-uuid", http.NoBody)
	if err = validate(bad); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("bad UUID accepted: %v", err)
	}
}

func TestHEADUsesTheExistingReadContract(t *testing.T) {
	t.Parallel()
	validate, err := contractValidator()
	if err != nil {
		t.Fatal(err)
	}
	const save = "/api/v1/saves/6dd5d52a-2702-4fa9-a56d-0573f16a68ea/payload"
	r := httptest.NewRequestWithContext(t.Context(), http.MethodHead, save, http.NoBody)
	if err = validate(r); err != nil {
		t.Fatalf("standard HEAD rejected GET contract: %v", err)
	}
	for _, path := range []string{"/api/v1/saves/not-a-uuid/payload", "/api/v1/auth/login"} {
		invalid := httptest.NewRequestWithContext(t.Context(), http.MethodHead, path, http.NoBody)
		if err = validate(invalid); err == nil {
			t.Fatalf("HEAD bypassed read contract for %s", path)
		}
	}
}

func TestResourceContractAcceptsSafeNamesAndRejectsEscapedUnsafeNames(t *testing.T) {
	t.Parallel()
	validate, err := contractValidator()
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "http://retrom.example/api/v1/runs/82df08fc-a299-4831-90a3-e998553f1d18/resources/12415b92-1fda-4754-bd31-e61cf73d185d/"
	cases := []struct {
		name  string
		valid bool
	}{
		{"nes-smoke.nes", true},
		{"1941.zip", true},
		{"音乐 #1.nes", true},
		{".hidden", true},
		{"...", true},
		{".", false},
		{"..", false},
		{"nested/game.nes", false},
		{`nested\game.nes`, false},
		{"game\x00.nes", false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			for _, rangeHeader := range []string{"", "bytes=0-63"} {
				r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, prefix+url.PathEscape(item.name), http.NoBody)
				r.Header.Set("Range", rangeHeader)
				validateErr := validate(r)
				if item.valid && validateErr != nil {
					t.Fatalf("safe resource name rejected: %v", validateErr)
				}
				if !item.valid && !errors.Is(validateErr, model.ErrInvalid) {
					t.Fatalf("unsafe resource name accepted: %v", validateErr)
				}
			}
		})
	}
}
