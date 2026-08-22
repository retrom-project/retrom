package hasheous

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"retrom/internal/testassert"
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
		testassert.Falsef(t, testassert.Any(func() bool { return request.URL.String() != lookupURL }, func() bool { return request.Method != http.MethodPost }), "request = %s %s", request.Method, request.URL)
		contents, _ := io.ReadAll(request.Body)
		requestBody = string(contents)
		return response(
			http.StatusOK,
			"application/json",
			`{"id":42,"name":"  <script>name</script>  ","publisher":{"name":"Pub"},"platform":{"name":"Game Boy Advance"},"signature":{"game":{"description":"Plain text","year":"2001","score":8},"rom":{"score":4}},"attributes":[{"attributeName":"Logo","attributeType":"ImageId","attributeRelationType":"None","value":"cover-1","link":"/api/v1/images/cover-1"},{"attributeName":"Tags","attributeType":"EmbeddedList","attributeRelationType":"None","value":{"GameGenre":{"Tags":[{"Text":"action"}]}}},{"attributeName":"Logo","attributeType":"ImageId","attributeRelationType":"None","value":"cover-2","link":"/api/v1/images/cover-2"}]}`,
		), nil
	})
	provider := New(client, nil, func() time.Time { return time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC) })
	result, err := provider.LookupByHash(context.Background(), ContentHashes{SHA256: strings.Repeat("a", 64)})
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Outcome != OutcomeHit }, func() bool { return result.Candidate == nil }, func() bool { return result.Candidate.ProviderGameID != "42" }), "result = %#v", result)
	testassert.Falsef(t, testassert.Any(func() bool { return requestBody != `{"shA256":"`+strings.Repeat("a", 64)+`"}` }, func() bool { return len(result.RequestDigest) != 64 }), "body/digest = %s / %s", requestBody, result.RequestDigest)
	testassert.Falsef(t, testassert.Any(func() bool { return result.Candidate.Metadata["title"] != "<script>name</script>" }, func() bool { return result.Candidate.Metadata["releaseYear"] != 2001 }), "metadata = %#v", result.Candidate.Metadata)
	testassert.Falsef(t, len(result.Candidate.Assets) != 1, "assets = %#v", result.Candidate.Assets)
	warnings, ok := result.Candidate.Evidence["warnings"].([]string)
	testassert.Truef(t, ok, "warnings type = %T", result.Candidate.Evidence["warnings"])
	testassert.Falsef(t, testassert.Any(func() bool { return len(warnings) != 1 }, func() bool { return warnings[0] != "DUPLICATE_ASSET_SLOT:COVER:0" }), "warnings = %#v", warnings)
}

func TestLookupUsesAIDescriptionWhenSignatureDescriptionIsEmpty(t *testing.T) {
	t.Parallel()
	client := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response(
			http.StatusOK,
			"application/json",
			`{"id":602921,"name":"1941 - Counter Attack","signature":{"game":{"description":""}},"attributes":[{"attributeName":"AIDescription","attributeType":"LongString","attributeRelationType":"None","value":"First paragraph.\r\n\r\nSecond\tparagraph."}]}`,
		), nil
	})
	provider := New(client, nil, func() time.Time { return time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC) })
	result, err := provider.LookupByHash(context.Background(), ContentHashes{SHA256: strings.Repeat("a", 64)})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return result.Candidate == nil }), "lookup result = %#v, error = %v", result, err)
	testassert.Falsef(t, result.Candidate.Metadata["description"] != "First paragraph.\n\nSecond\tparagraph.", "description = %#v", result.Candidate.Metadata["description"])
	warnings, ok := result.Candidate.Evidence["warnings"].([]string)
	testassert.Falsef(t, testassert.Any(func() bool { return !ok }, func() bool { return !slices.Contains(warnings, "FIELD_FALLBACK:description:AIDescription") }), "warnings = %#v", result.Candidate.Evidence["warnings"])
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
			testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return result.Outcome != test.want }), "result/err = %#v / %v", result, err)
		})
	}
}

func TestFetchAssetValidatesImageAndEveryRedirect(t *testing.T) {
	t.Parallel()
	pngBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	testassert.False(t, err != nil, err)
	public := resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		testassert.Falsef(t, host != "hasheous.org", "resolved host = %s", host)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return asset.MediaType != "image/png" }, func() bool { return asset.Width != 1 }, func() bool { return asset.Height != 1 }), "asset/err = %#v / %v", asset, err)

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

func TestValidateImageUsesDecodedBytesWhenSupportedImageHeaderIsMislabeled(t *testing.T) {
	t.Parallel()
	pngBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	testassert.False(t, err != nil, err)
	asset, err := ValidateImage(pngBytes, "image/jpeg; x-api-version=1")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return asset.MediaType != "image/png" }, func() bool { return asset.Width != 1 }, func() bool { return asset.Height != 1 }), "mislabeled supported image = %#v, error=%v", asset, err)
	if _, err := ValidateImage(pngBytes, "text/html"); !errors.Is(err, ErrAssetMediaTypeMismatch) {
		t.Fatalf("non-image declared media type error = %v", err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return result.Outcome != OutcomeInvalidResponse }), "result/err = %#v / %v", result, err)
}

func TestRestoreCachedRevalidatesHitAndMiss(t *testing.T) {
	t.Parallel()
	hashes := ContentHashes{SHA256: strings.Repeat("a", 64)}
	provider := New(nil, nil, func() time.Time { return time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC) })
	raw := []byte(`{"id":42,"name":"Cached","signature":{"game":{"year":"2001"}}}`)
	hit, err := provider.RestoreCached(hashes, OutcomeHit, http.StatusOK, raw)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return hit.Candidate == nil }, func() bool { return hit.Candidate.Metadata["title"] != "Cached" }, func() bool { return len(hit.RequestDigest) != 64 }), "cached hit = %#v, error=%v", hit, err)
	miss, err := provider.RestoreCached(hashes, OutcomeMiss, http.StatusNotFound, nil)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return miss.Outcome != OutcomeMiss }, func() bool { return miss.Candidate != nil }, func() bool { return miss.RequestDigest != hit.RequestDigest }), "cached miss = %#v, error=%v", miss, err)
	if _, err := provider.RestoreCached(hashes, OutcomeInvalidResponse, http.StatusOK, raw); err == nil {
		t.Fatal("non-cacheable response restored")
	}
}
