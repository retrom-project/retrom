package httpapi

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestParentOptionsContractRequiresCoreAndCanonicalGameID(t *testing.T) {
	t.Parallel()
	validate, err := contractValidator()
	if err != nil {
		t.Fatal(err)
	}
	for query, valid := range map[string]bool{"?coreId=fbneo": true, "": false, "?coreId=": false} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			"/api/v1/admin/games/82df08fc-a299-4831-90a3-e998553f1d18/runtime-options/arcade"+query, http.NoBody)
		if err := validate(r); (err == nil) != valid {
			t.Fatalf("query=%q valid=%t error=%v", query, valid, err)
		}
	}
}

func TestParentFailureHTTPPreservesTheRuntimeMissingName(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeError(w, &model.ArcadeParentError{Code: "RUNTIME_PARENT_MISSING", Message: "Required Parent ROM is missing: 1941"})
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest || body["code"] != "RUNTIME_PARENT_MISSING" || !strings.Contains(body["message"], "1941") {
		t.Fatalf("parent failure=%d %s", w.Code, w.Body.String())
	}
}

func TestParentMultipartMetadataUsesAuthorityAndSingleValues(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		version string
		cores   []string
		valid   bool
	}{
		{"1", []string{"fbneo"}, true},
		{"0", []string{"fbneo"}, false},
		{"1", []string{""}, false},
		{"1", []string{"fbneo", "mame2003"}, false},
	} {
		form := &multipart.Form{
			Value: map[string][]string{"version": {item.version}, "coreId": item.cores},
			File:  map[string][]*multipart.FileHeader{"file": {{Filename: "1941.zip"}}},
		}
		if _, _, err := parentUploadMetadata(form); (err == nil) != item.valid {
			t.Fatalf("metadata valid=%t error=%v", item.valid, err)
		}
	}
}
