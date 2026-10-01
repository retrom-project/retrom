package httpapi

import (
	"math"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
)

// Limits are protocol metadata and are installed before OpenAPI or any handler reads the body.
func requestBodyLimit(operation *openapi3.Operation) int64 {
	if operation.RequestBody == nil {
		return 0
	}
	value, ok := operation.Extensions["x-retrom-max-body-bytes"].(float64)
	if !ok || value <= 0 || value > 270<<20 || math.IsNaN(value) || math.Trunc(value) != value {
		panic("OpenAPI request body requires a valid x-retrom-max-body-bytes: " + operation.OperationID)
	}
	return int64(value)
}

func writeRequestTooLarge(writer http.ResponseWriter, request *http.Request) {
	writeError(writer, request, http.StatusRequestEntityTooLarge,
		"REQUEST_TOO_LARGE", "请求内容超过限制", map[string]any{})
}

func boundOpenAPIRequestBody(writer http.ResponseWriter, request *http.Request, operation *openapi3.Operation) bool {
	limit := requestBodyLimit(operation)
	if request.ContentLength > limit && operation.RequestBody != nil {
		writeRequestTooLarge(writer, request)
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, limit)
	if operation.RequestBody == nil && request.ContentLength > 0 {
		writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "请求不允许包含 body", map[string]any{})
		return false
	}
	return true
}
