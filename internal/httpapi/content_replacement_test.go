package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestContentReplacementContractValidatesOperationsAndSubmission(t *testing.T) {
	t.Parallel()
	validate, err := contractValidator()
	if err != nil {
		t.Fatal(err)
	}
	const base = "/api/v1/admin/games/6dd5d52a-2702-4fa9-a56d-0573f16a68ea/content-replacement"
	const operation = "/82df08fc-a299-4831-90a3-e998553f1d18"
	for _, item := range []struct {
		method, path, body string
		valid              bool
	}{
		{http.MethodGet, base, "", true},
		{http.MethodGet, base + operation, "", true},
		{http.MethodPost, base + operation + "/cancel", "", true},
		{http.MethodPost, base, `{"path":"/mounted/new.zip","version":2}`, true},
		{http.MethodGet, base + "/bad-id", "", false},
		{http.MethodPost, base + "/bad-id/cancel", "", false},
		{http.MethodPost, base, `{"path":"relative.zip","version":2}`, false},
		{http.MethodPost, base, `{"path":"/mounted/new.zip","version":0}`, false},
		{http.MethodPost, base, `{"path":"/mounted/new.zip"}`, false},
	} {
		r := httptest.NewRequestWithContext(t.Context(), item.method, item.path, strings.NewReader(item.body))
		if item.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		err = validate(r)
		if item.valid && err != nil || !item.valid && !errors.Is(err, model.ErrInvalid) {
			t.Errorf("%s %s valid=%t err=%v", item.method, item.path, item.valid, err)
		}
	}
}
