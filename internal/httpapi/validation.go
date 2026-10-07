package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"retrom/internal/httpapi/generated"
	"retrom/internal/model"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

func contractValidator() (func(*http.Request) error, error) {
	spec, err := generated.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load HTTP contract: %w", err)
	}
	spec.Servers = nil
	router, err := legacy.NewRouter(spec)
	if err != nil {
		return nil, fmt.Errorf("construct HTTP contract router: %w", err)
	}
	return func(r *http.Request) error {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			return nil
		}
		routingRequest := r.Clone(r.Context())
		// ServeMux provides HEAD for every GET route; validate that same read contract.
		if routingRequest.Method == http.MethodHead {
			routingRequest.Method = http.MethodGet
		}
		routingURL := *r.URL
		routingURL.Path = r.URL.EscapedPath()
		routingURL.RawPath = ""
		routingRequest.URL = &routingURL
		route, params, routeErr := router.FindRoute(routingRequest)
		if routeErr != nil {
			return model.ErrNotFound
		}
		for key, value := range params {
			decoded, decodeErr := url.PathUnescape(value)
			if decodeErr != nil {
				return model.ErrInvalid
			}
			params[key] = decoded
		}
		if formatErr := parameterFormats(route, params, r); formatErr != nil {
			return formatErr
		}
		multipart := strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
		if !multipart {
			r.Body = http.MaxBytesReader(nil, r.Body, 1024*1024)
		}
		options := &openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			ExcludeRequestBody: multipart,
			SchemaValidationOptions: []openapi3.SchemaValidationOption{
				openapi3.EnableFormatValidation(),
				openapi3.WithStringFormatValidator("uuid", openapi3.NewCallbackValidator(validateUUID)),
			},
		}
		input := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: options}
		if validateErr := openapi3filter.ValidateRequest(r.Context(), input); validateErr != nil {
			return model.ErrInvalid
		}
		return nil
	}, nil
}

func parameterFormats(route *routers.Route, params map[string]string, r *http.Request) error {
	parameters := append(append([]*openapi3.ParameterRef{}, route.PathItem.Parameters...), route.Operation.Parameters...)
	for _, ref := range parameters {
		parameter := ref.Value
		if parameter == nil || parameter.Schema == nil || parameter.Schema.Value == nil {
			continue
		}
		if parameter.Schema.Value.Format != "uuid" {
			continue
		}
		value := params[parameter.Name]
		if parameter.In == "query" {
			value = r.URL.Query().Get(parameter.Name)
		}
		if value != "" && !model.UUID(value) {
			return model.ErrInvalid
		}
	}
	return nil
}

func validateUUID(value string) error {
	if !model.UUID(value) {
		return model.ErrInvalid
	}
	return nil
}

func saveMetadataValidator() (func(string, *model.SaveInput) error, error) {
	spec, err := generated.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load save metadata contract: %w", err)
	}
	schema := spec.Components.Schemas["SaveCommitMetadata"]
	if schema == nil || schema.Value == nil {
		return nil, fmt.Errorf("save metadata contract is missing: %w", model.ErrInvalid)
	}
	return func(raw string, input *model.SaveInput) error {
		if len(raw) > 1024*1024 {
			return model.ErrInvalid
		}
		if err := strictJSON(strings.NewReader(raw), input); err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return model.ErrInvalid
		}
		if err := schema.Value.VisitJSON(value, openapi3.EnableFormatValidation(),
			openapi3.WithStringFormatValidator("uuid", openapi3.NewCallbackValidator(validateUUID))); err != nil {
			return model.ErrInvalid
		}
		return nil
	}, nil
}

func strictJSON(reader io.Reader, value any) error {
	raw, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read JSON body: %w", model.ErrInvalid)
	}
	if !utf8.Valid(raw) {
		return model.ErrInvalid
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if err = uniqueValue(tokens, 0); err != nil {
		return err
	}
	if _, err = tokens.Token(); !errors.Is(err, io.EOF) {
		return model.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return model.ErrInvalid
	}
	return nil
}

func uniqueValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return model.ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return model.ErrInvalid
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			key, readErr := decoder.Token()
			if readErr != nil {
				return model.ErrInvalid
			}
			name, valid := key.(string)
			if !valid || seen[name] {
				return model.ErrInvalid
			}
			seen[name] = true
			if err = uniqueValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err = uniqueValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return model.ErrInvalid
	}
	if _, err = decoder.Token(); err != nil {
		return model.ErrInvalid
	}
	return nil
}

func boundedRequest(r *http.Request) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	return r.WithContext(ctx), cancel
}
