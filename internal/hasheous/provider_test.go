package hasheous

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) Do(request *http.Request) (*http.Response, error) {
	return function(request)
}

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (function resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return function(ctx, host)
}

func response(status int, mediaType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{mediaType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestLookupNormalizesBoundedResponse(t *testing.T) {
	t.Parallel()
	var requestBody string
	client := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != lookupURL || request.Method != http.MethodPost {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		contents, _ := io.ReadAll(request.Body)
		requestBody = string(contents)
		return response(
			http.StatusOK,
			"application/json",
			`{"id":42,"name":"  <script>name</script>  ","publisher":{"name":"Pub"},"platform":{"name":"Game Boy Advance"},"signature":{"game":{"description":"Plain text","year":"2001","score":8},"rom":{"score":4}},"attributes":[{"name":"Logo","attributeType":"ImageId","attributeRelationType":"None","value":"cover-1","link":"/api/v1/images/cover-1"},{"name":"Logo","attributeType":"ImageId","attributeRelationType":"None","value":"cover-2","link":"/api/v1/images/cover-2"}]}`,
		), nil
	})
	provider := New(client, nil, func() time.Time { return time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC) })
	result, err := provider.LookupByHash(context.Background(), ContentHashes{SHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeHit || result.Candidate == nil || result.Candidate.ProviderGameID != "42" {
		t.Fatalf("result = %#v", result)
	}
	if requestBody != `{"shA256":"`+strings.Repeat("a", 64)+`"}` || len(result.RequestDigest) != 64 {
		t.Fatalf("body/digest = %s / %s", requestBody, result.RequestDigest)
	}
	if result.Candidate.Metadata["title"] != "<script>name</script>" ||
		result.Candidate.Metadata["releaseYear"] != 2001 {
		t.Fatalf("metadata = %#v", result.Candidate.Metadata)
	}
	if len(result.Candidate.Assets) != 1 {
		t.Fatalf("assets = %#v", result.Candidate.Assets)
	}
	warnings, ok := result.Candidate.Evidence["warnings"].([]string)
	if !ok {
		t.Fatalf("warnings type = %T", result.Candidate.Evidence["warnings"])
	}
	if len(warnings) != 1 || warnings[0] != "DUPLICATE_ASSET_SLOT:COVER:0" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestLookupClassifiesMissAndOversize(t *testing.T) {
	t.Parallel()
	hash := ContentHashes{CRC32: "1234abcd"}
	for _, test := range []struct {
		name string
		body string
		code int
		want ProviderOutcome
	}{{"miss text", "not found", http.StatusNotFound, OutcomeMiss}, {"oversize", strings.Repeat("x", maximumBodySize+1), http.StatusOK, OutcomeInvalidResponse}, {"server", "", http.StatusBadGateway, OutcomeNetworkError}, {"client", "", http.StatusBadRequest, OutcomeInvalidResponse}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := New(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return response(test.code, "text/plain", test.body), nil
			}), nil, time.Now)
			result, err := provider.LookupByHash(context.Background(), hash)
			if err != nil || result.Outcome != test.want {
				t.Fatalf("result/err = %#v / %v", result, err)
			}
		})
	}
}

func TestFetchAssetValidatesImageAndEveryRedirect(t *testing.T) {
	t.Parallel()
	pngBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	public := resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "hasheous.org" {
			t.Fatalf("resolved host = %s", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})
	provider := New(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader(string(pngBytes))),
		}, nil
	}), public, time.Now)
	asset, err := provider.FetchAsset(
		context.Background(),
		AssetRef{ProviderAssetID: "one", Path: "/api/v1/images/one"},
	)
	if err != nil || asset.MediaType != "image/png" || asset.Width != 1 || asset.Height != 1 {
		t.Fatalf("asset/err = %#v / %v", asset, err)
	}

	redirecting := New(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		redirect := response(http.StatusFound, "text/plain", "")
		redirect.Header.Set("Location", "https://127.0.0.1/private")
		return redirect, nil
	}), public, time.Now)
	if _, err := redirecting.FetchAsset(context.Background(), AssetRef{ProviderAssetID: "one", Path: "/api/v1/images/one"}); err == nil ||
		err.Error() != "ASSET_URL_REJECTED" {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestLookupRejectsInvalidHashAndCandidate(t *testing.T) {
	t.Parallel()
	provider := New(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response(http.StatusOK, "application/json", `{"id":0,"name":"bad"}`), nil
	}), nil, time.Now)
	if _, err := provider.LookupByHash(context.Background(), ContentHashes{SHA1: "ABC"}); err == nil {
		t.Fatal("invalid hash accepted")
	}
	result, err := provider.LookupByHash(context.Background(), ContentHashes{MD5: strings.Repeat("0", 32)})
	if err != nil || result.Outcome != OutcomeInvalidResponse {
		t.Fatalf("result/err = %#v / %v", result, err)
	}
}

func TestRestoreCachedRevalidatesHitAndMiss(t *testing.T) {
	t.Parallel()
	hashes := ContentHashes{SHA256: strings.Repeat("a", 64)}
	provider := New(nil, nil, func() time.Time { return time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC) })
	raw := []byte(`{"id":42,"name":"Cached","signature":{"game":{"year":"2001"}}}`)
	hit, err := provider.RestoreCached(hashes, OutcomeHit, http.StatusOK, raw)
	if err != nil || hit.Candidate == nil || hit.Candidate.Metadata["title"] != "Cached" ||
		len(hit.RequestDigest) != 64 {
		t.Fatalf("cached hit = %#v, error=%v", hit, err)
	}
	miss, err := provider.RestoreCached(hashes, OutcomeMiss, http.StatusNotFound, nil)
	if err != nil || miss.Outcome != OutcomeMiss || miss.Candidate != nil || miss.RequestDigest != hit.RequestDigest {
		t.Fatalf("cached miss = %#v, error=%v", miss, err)
	}
	if _, err := provider.RestoreCached(hashes, OutcomeInvalidResponse, http.StatusOK, raw); err == nil {
		t.Fatal("non-cacheable response restored")
	}
}
