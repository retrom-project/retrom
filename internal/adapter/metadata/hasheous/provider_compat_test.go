package hasheous

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	metadatamodel "retrom/internal/model/metadata"
)

func TestLookupPreservesRawAndNormalizedJSONBytes(t *testing.T) {
	t.Parallel()
	raw := []byte(" \n{\"id\":42,\"name\":\"  <script>name</script>  \",\"publisher\":{\"name\":\"Pub\"},\"platform\":{\"name\":\"Game Boy Advance\"},\"signature\":{\"game\":{\"description\":\"Plain text\",\"year\":\"2001\",\"score\":8},\"rom\":{\"score\":4}},\"attributes\":[{\"attributeName\":\"Logo\",\"attributeType\":\"ImageId\",\"attributeRelationType\":\"None\",\"value\":\"cover-1\",\"link\":\"/api/v1/images/cover-1\"},{\"attributeName\":\"Tags\",\"attributeType\":\"EmbeddedList\",\"attributeRelationType\":\"None\",\"value\":{\"GameGenre\":{\"Tags\":[{\"Text\":\"action\"}]}}},{\"attributeName\":\"Logo\",\"attributeType\":\"ImageId\",\"attributeRelationType\":\"None\",\"value\":\"cover-2\",\"link\":\"/api/v1/images/cover-2\"}],\"unknown\":{\"nested\":[1,\"raw\"]}}\t\n")
	provider := New(roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response(http.StatusOK, "application/json", string(raw)), nil
	}), nil, func() time.Time { return time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC) })
	result, err := provider.LookupByHash(t.Context(), metadatamodel.ContentHashes{SHA256: strings.Repeat("a", 64)})
	if err != nil || result.Candidate == nil {
		t.Fatalf("lookup: %#v / %v", result, err)
	}
	if !bytes.Equal(result.RawResponse, raw) {
		t.Fatalf("raw response changed: %q", result.RawResponse)
	}
	if result.RequestDigest != "6104b845bb264870bfa0b9b387581882127cf16384ac89b0f4f51e0e3109cd19" {
		t.Fatalf("request digest = %s", result.RequestDigest)
	}
	encoded, err := json.Marshal(result.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"providerGameId":"42","metadata":{"description":"Plain text","developer":"","genre":"","players":null,"publisher":"Pub","releaseYear":2001,"schemaVersion":1,"title":"\u003cscript\u003ename\u003c/script\u003e"},"evidence":{"normalizationYear":2026,"normalizerVersion":"HASHEOUS_BY_HASH_V1","platformName":"Game Boy Advance","providerGameScore":8,"providerRomScore":4,"schemaVersion":1,"warnings":["DUPLICATE_ASSET_SLOT:COVER:0"]},"assets":[{"providerAssetId":"cover-1","kind":"COVER","ordinal":0,"path":"/api/v1/images/cover-1"}]}` {
		t.Fatalf("normalized candidate changed:\n%s", encoded)
	}
	cached, err := provider.RestoreCached(metadatamodel.ContentHashes{SHA256: strings.Repeat("a", 64)}, metadatamodel.OutcomeHit, encodeHTTPAudit(http.StatusOK), raw)
	if err != nil || cached.Candidate == nil || !bytes.Equal(cached.RawResponse, raw) {
		t.Fatalf("cached raw: %#v / %v", cached, err)
	}
	restored, err := json.Marshal(cached.Candidate)
	if err != nil || !bytes.Equal(restored, encoded) {
		t.Fatalf("cached normalized JSON: %s / %v", restored, err)
	}
}

func TestCandidatePreservesNullAndEmptyJSON(t *testing.T) {
	t.Parallel()
	for _, expected := range []string{
		`{"providerGameId":"","metadata":null,"evidence":null,"assets":null}`,
		`{"providerGameId":"","metadata":{},"evidence":{},"assets":[]}`,
	} {
		t.Run(expected, func(t *testing.T) {
			var candidate metadatamodel.Candidate
			if err := json.Unmarshal([]byte(expected), &candidate); err != nil {
				t.Fatal(err)
			}
			actual, err := json.Marshal(candidate)
			if err != nil || string(actual) != expected {
				t.Fatalf("nil/empty encoding: %s / %v", actual, err)
			}
		})
	}
}
