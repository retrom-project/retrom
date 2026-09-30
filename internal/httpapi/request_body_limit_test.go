package httpapi

import (
	"math"
	"testing"

	"retrom/internal/httpapi/generated"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestRequestBodyLimitsRejectMissingOrInvalidMetadata(t *testing.T) {
	for _, value := range []any{nil, "32768", float64(0), float64(-1), 1.5, float64((270 << 20) + 1), math.NaN(), math.Inf(1)} {
		t.Run("invalid", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("accepted body cap %v", value)
				}
			}()
			requestBodyLimit(&openapi3.Operation{RequestBody: &openapi3.RequestBodyRef{}, Extensions: map[string]any{
				"x-retrom-max-body-bytes": value,
			}})
		})
	}
}

func TestEveryOpenAPIRequestBodyHasBoundedParsing(t *testing.T) {
	specification, err := generated.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range specification.Paths.Map() {
		for method, operation := range item.Operations() {
			limit := requestBodyLimit(operation)
			if operation.RequestBody != nil && limit <= 0 {
				t.Fatalf("unbounded %s %s", method, path)
			}
			if operation.RequestBody == nil && limit != 0 {
				t.Fatalf("body admitted %s %s", method, path)
			}
			if path == "/api/v1/auth/login" && method == "POST" && limit != 32<<10 {
				t.Fatalf("login pre-parser cap=%d", limit)
			}
		}
	}
}
