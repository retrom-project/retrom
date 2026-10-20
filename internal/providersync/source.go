package providersync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ErrFetchFailed reports a release asset that could not be retrieved.
var ErrFetchFailed = errors.New("PROVIDER_FETCH_FAILED")

// Source retrieves flat release assets by name. Identity always comes from the
// pinned digests, never from the transport location.
type Source interface {
	Fetch(ctx context.Context, name string) (io.ReadCloser, error)
}

// NewSource selects a local directory or an HTTP(S) base. An empty location
// derives the GitHub Release of a tagged pin.
func NewSource(location string, inputs *Inputs, client *http.Client) (Source, error) {
	if location == "" {
		if inputs.Release == nil {
			return nil, ErrSourceRequired
		}
		location = strings.TrimSuffix(inputs.Release.Repository, "/") + "/releases/download/" + inputs.Release.Tag
	}
	if strings.HasPrefix(location, "https://") || strings.HasPrefix(location, "http://") {
		if client == nil {
			client = http.DefaultClient
		}
		return httpSource{base: strings.TrimSuffix(location, "/"), client: client}, nil
	}
	absolute, err := filepath.Abs(location)
	if err != nil {
		return nil, fmt.Errorf("resolve chunk source: %w", err)
	}
	return directorySource{root: absolute}, nil
}

type directorySource struct{ root string }

func (s directorySource) Fetch(_ context.Context, name string) (io.ReadCloser, error) {
	if !assetName.MatchString(name) {
		return nil, ErrInputsInvalid
	}
	file, err := os.Open(filepath.Join(s.root, name))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w: %w", name, ErrFetchFailed, err)
	}
	return file, nil
}

type httpSource struct {
	base   string
	client *http.Client
}

func (s httpSource) Fetch(ctx context.Context, name string) (io.ReadCloser, error) {
	if !assetName.MatchString(name) {
		return nil, ErrInputsInvalid
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", name, err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w: %w", name, ErrFetchFailed, err)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %d: %w", name, response.StatusCode, ErrFetchFailed)
	}
	return response.Body, nil
}
