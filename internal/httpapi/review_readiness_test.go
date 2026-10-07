package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewReadinessContractBindsTheBoundedBatch(t *testing.T) {
	t.Parallel()
	validate, err := contractValidator()
	if err != nil {
		t.Fatal(err)
	}
	const id = "82df08fc-a299-4831-90a3-e998553f1d18"
	ids := make([]string, 101)
	for index := range ids {
		ids[index] = fmt.Sprintf("82df08fc-a299-4831-90a3-%012d", index)
	}
	oversized, err := json.Marshal(map[string]any{"gameIds": ids})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		body  string
		valid bool
	}{
		{"oversized", string(oversized), false},
		{"single", `{"gameIds":["` + id + `"]}`, true},
		{"empty", `{"gameIds":[]}`, false},
		{"duplicate", `{"gameIds":["` + id + `","` + id + `"]}`, false},
		{"invalid id", `{"gameIds":["invalid"]}`, false},
		{"extra property", `{"gameIds":["` + id + `"],"approve":true}`, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/reviews/readiness", strings.NewReader(item.body))
			request.Header.Set("Content-Type", "application/json")
			if err := validate(request); (err == nil) != item.valid {
				t.Fatalf("valid=%t error=%v", item.valid, err)
			}
		})
	}
}
