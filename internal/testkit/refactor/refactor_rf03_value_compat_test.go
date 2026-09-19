package refactor

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/metadata/hasheous"
	blobmodel "retrom/internal/model/blob"
	metadatamodel "retrom/internal/model/metadata"
	"retrom/internal/model/netplayprofile"
	savesmodel "retrom/internal/model/saves"
)

type rf03ValueHTTP struct{ body, method, url, requestBody string }

func (client *rf03ValueHTTP) Do(request *http.Request) (*http.Response, error) {
	contents, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	client.method, client.url, client.requestBody = request.Method, request.URL.String(), string(contents)
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewReader([]byte(client.body))),
	}, nil
}

func rf03MetadataValueCompatibility(t *testing.T, input rf03ValueInput, expected rf03CapturedMetadata) {
	t.Helper()
	now, err := time.Parse(time.RFC3339Nano, input.FixedNow)
	if err != nil {
		t.Fatal(err)
	}
	client := &rf03ValueHTTP{body: input.HTTPRawBody}
	provider := hasheous.New(client, nil, func() time.Time { return now })
	result, err := provider.LookupByHash(t.Context(), input.Hashes)
	if err != nil || string(result.Outcome) != expected.Outcome || result.Candidate == nil {
		t.Fatalf("metadata result = %+v / %v", result, err)
	}
	t.Run("normalized_candidate", func(t *testing.T) { rf03AssertJSONBytes(t, result.Candidate, expected.CandidateJSON) })
	t.Run("opaque_raw_response", func(t *testing.T) {
		rf03AssertRawResponse(t, input.HTTPRawBody, result.RawResponse, expected.RawResponseBase64)
	})
	t.Run("request_identity", func(t *testing.T) { rf03AssertRequestIdentity(t, input, result, client, expected) })
	t.Run("null_fields", func(t *testing.T) { rf03AssertJSONBytes(t, metadatamodel.Candidate{}, expected.NullCandidateJSON) })
	t.Run("empty_fields", func(t *testing.T) {
		candidate := metadatamodel.Candidate{
			Metadata: json.RawMessage(`{}`), Evidence: json.RawMessage(`{}`), Assets: []metadatamodel.AssetReference{},
		}
		rf03AssertJSONBytes(t, candidate, expected.EmptyCandidateJSON)
	})
}

func rf03AssertRawResponse(t *testing.T, input string, actual []byte, encoded string) {
	t.Helper()
	frozen, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frozen, []byte(input)) || !bytes.Equal(actual, frozen) {
		t.Fatalf("raw upstream bytes changed:\nactual: %q\nfrozen: %q", actual, frozen)
	}
	if !bytes.Contains(actual, []byte(`"unknownUpstream"`)) || !bytes.HasPrefix(actual, []byte(" \n")) || !bytes.HasSuffix(actual, []byte("\t\n")) {
		t.Fatal("opaque evidence lost unknown fields or surrounding whitespace")
	}
}

func rf03AssertRequestIdentity(t *testing.T, input rf03ValueInput, result metadatamodel.LookupResult,
	client *rf03ValueHTTP, expected rf03CapturedMetadata,
) {
	t.Helper()
	digest, err := metadatamodel.RequestDigest(input.Hashes)
	if err != nil || digest != expected.HashDigest || result.RequestDigest != expected.RequestDigest {
		t.Fatalf("digest identity changed: %s / %s / %v", digest, result.RequestDigest, err)
	}
	if string(result.RequestBody) != expected.RequestBody || client.requestBody != expected.RequestBody ||
		client.method != expected.RequestMethod || client.url != expected.RequestURL {
		t.Fatalf("HTTP request changed: %+v", client)
	}
}

func rf03JobValueCompatibility(t *testing.T, input rf03ValueInput, golden rf03CapturedValues) {
	t.Helper()
	for index, job := range input.Jobs {
		t.Run(job.State, func(t *testing.T) { rf03AssertJSONBytes(t, job, golden.JobJSON[index]) })
	}
}

func rf03SaveValueCompatibility(t *testing.T, input rf03ValueInput, golden rf03CapturedValues) {
	t.Helper()
	for index, input := range input.Saves {
		t.Run(input.Label, func(t *testing.T) {
			if input.Label != golden.Saves[index].Label {
				t.Fatal("save input/output cases do not match")
			}
			value := savesmodel.ManualResult{
				ResourceKind: input.ResourceKind, SaveStateID: input.SaveStateID, PreviewID: input.PreviewID,
				CheckpointFormat: input.CheckpointFormat, ScreenshotURL: input.ScreenshotURL, CreatedAtMS: input.CreatedAtMS,
				Name: input.Name, DiscIndex: input.DiscIndex, Version: input.Version, ActiveDurationMS: input.ActiveDurationMS,
			}
			rf03AssertJSONBytes(t, value, golden.Saves[index].JSON)
			var restored savesmodel.ManualResult
			if err := json.Unmarshal([]byte(golden.Saves[index].JSON), &restored); err != nil {
				t.Fatal(err)
			}
			rf03AssertJSONBytes(t, restored, golden.Saves[index].JSON)
		})
	}
}

func rf03BlobValueCompatibility(t *testing.T, body, expected string) {
	t.Helper()
	store, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var prepared blobmodel.PreparedBlob
	prepared, err = store.Put(bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	rf03AssertJSONBytes(t, prepared, expected)
}

func rf03NetplayValueCompatibility(t *testing.T) {
	t.Helper()
	var golden struct{ RawManifestSHA256, ProtocolJSON, ManifestJSON string }
	rf03ReadJSON(t, filepath.Join("..", "..", "model", "netplayprofile", "testdata", "registry-golden.json"), &golden)
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "data", "netplay", "v2", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != golden.RawManifestSHA256 {
		t.Fatal("netplay source manifest changed from its old capture")
	}
	var manifest netplayprofile.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	rf03AssertJSONBytes(t, manifest.Protocol, golden.ProtocolJSON)
	rf03AssertJSONBytes(t, manifest, golden.ManifestJSON)
}
