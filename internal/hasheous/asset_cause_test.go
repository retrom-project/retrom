package hasheous

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func assetTestNow() time.Time { return time.Date(2028, 4, 5, 6, 7, 8, 0, time.UTC) }
func publicAssetResolver(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
}

var assetTestRef = AssetRef{ProviderAssetID: "fixture", Path: "/api/v1/images/fixture"}

type assetReadFailure struct{ cause error }

func (reader assetReadFailure) Read(buffer []byte) (int, error) {
	return copy(buffer, "bad"), reader.cause
}
func (assetReadFailure) Close() error { return nil }

func TestFetchAssetPreservesTransportAndDNSCauses(t *testing.T) {
	transport := errors.New("transport failed")
	dns := &net.DNSError{Err: "lookup failed", Name: "hasheous.org"}
	for _, test := range []struct {
		name          string
		resolver      Resolver
		client        HTTPDoer
		cause, stable error
	}{
		{"transport", resolverFunc(publicAssetResolver), roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, transport }), transport, ErrAssetNetwork},
		{"DNS", resolverFunc(func(context.Context, string) ([]net.IPAddr, error) { return nil, dns }), nil, dns, ErrAssetDNSFailed},
		{"read", resolverFunc(publicAssetResolver), roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: assetReadFailure{io.ErrUnexpectedEOF}}, nil
		}), io.ErrUnexpectedEOF, ErrAssetNetwork},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.client, test.resolver, assetTestNow).FetchAsset(t.Context(), assetTestRef)
			if !errors.Is(err, test.cause) || !errors.Is(err, test.stable) {
				t.Fatalf("lost underlying asset cause: %v", err)
			}
		})
	}
}
