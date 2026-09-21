package httpapi

import (
	"context"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"retrom/internal/httpapi/generated"
)

func handleOpenAPIValidationError(
	_ context.Context,
	_ error,
	writer http.ResponseWriter,
	request *http.Request,
	options nethttpmiddleware.ErrorHandlerOpts,
) {
	if options.MatchedRoute == nil && options.StatusCode == http.StatusNotFound {
		writeError(writer, request, http.StatusNotFound, "RESOURCE_NOT_FOUND", "请求的资源不存在", map[string]any{})
		return
	}
	if options.MatchedRoute != nil && request.Header.Get("If-Match") == "" &&
		operationRequiresHeader(options.MatchedRoute.Route.Operation, "If-Match") {
		writeError(
			writer,
			request,
			http.StatusPreconditionRequired,
			"PRECONDITION_REQUIRED",
			"需要当前资源版本",
			map[string]any{},
		)
		return
	}
	if options.MatchedRoute != nil && request.Header.Get("Idempotency-Key") == "" &&
		operationRequiresHeader(options.MatchedRoute.Route.Operation, "Idempotency-Key") {
		writeError(
			writer,
			request,
			http.StatusBadRequest,
			"INVALID_IDEMPOTENCY_KEY",
			"需要有效的幂等键",
			map[string]any{},
		)
		return
	}
	writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "请求不符合 API 契约", map[string]any{})
}

func (server *Server) openAPIHandler(next http.Handler) http.Handler {
	specification, err := generated.GetSpec()
	if err != nil {
		panic("load generated OpenAPI specification: " + err.Error())
	}
	specification.Servers = nil
	for documentedPath, pathItem := range specification.Paths.Map() {
		routerPath, ok := pathItem.Extensions["x-retrom-router-template"].(string)
		if !ok || routerPath == "" || routerPath == documentedPath {
			continue
		}
		specification.Paths.Set(routerPath, pathItem)
		specification.Paths.Delete(documentedPath)
	}
	router, err := gorillamux.NewRouter(specification)
	if err != nil {
		panic("build OpenAPI operation router: " + err.Error())
	}
	normalOptions := &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			// Authentication and authorization are enforced by accountAuthHandler. The
			// schema validator only validates protocol shape, so it must not try to
			// interpret the opaque session cookie a second time.
			AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error { return nil },
			MultiError:         true,
		},
		ErrorHandlerWithOpts: handleOpenAPIValidationError,
		DoNotValidateServers: true,
	}
	streamingOptions := &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error { return nil },
			ExcludeRequestBody: true,
			MultiError:         true,
		},
		ErrorHandlerWithOpts: handleOpenAPIValidationError,
		DoNotValidateServers: true,
	}
	validatedNext := server.idempotencyHandler(next)
	normal := nethttpmiddleware.OapiRequestValidatorWithOptions(specification, normalOptions)(validatedNext)
	streaming := nethttpmiddleware.OapiRequestValidatorWithOptions(specification, streamingOptions)(validatedNext)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		route, _, routeErr := router.FindRoute(request)
		if routeErr == nil && route.Operation != nil {
			request = request.WithContext(
				context.WithValue(request.Context(), operationIDContextKey, route.Operation.OperationID),
			)
			if route.Operation.RequestBody == nil && request.ContentLength > 0 {
				writeError(writer, request, http.StatusBadRequest, "INVALID_REQUEST", "请求不允许包含 body", map[string]any{})
				return
			}
			if enabled, ok := route.Operation.Extensions["x-retrom-streaming-body"].(bool); ok && enabled {
				streaming.ServeHTTP(writer, request)
				return
			}
		}
		normal.ServeHTTP(writer, request)
	})
}

func operationRequiresHeader(operation *openapi3.Operation, name string) bool {
	if operation == nil {
		return false
	}
	for _, parameter := range operation.Parameters {
		if parameter.Value != nil && parameter.Value.In == openapi3.ParameterInHeader && parameter.Value.Required &&
			parameter.Value.Name == name {
			return true
		}
	}
	return false
}
