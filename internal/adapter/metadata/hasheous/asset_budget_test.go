package hasheous

import (
	"context"
	"errors"
	"net/http"
	"testing"

	metadatamodel "retrom/internal/model/metadata"
)

func TestBoundedAssetReportsInvalidAndFailedResponseBytes(t *testing.T) {
	for _, test := range []struct {
		name    string
		body    string
		failure error
		limit   int64
		bytes   int64
		cause   error
	}{
		{"invalid image", "not an image", nil, 100, 12, metadatamodel.ErrAssetMediaTypeInvalid},
		{"partial read", "", context.Canceled, 100, 3, context.Canceled},
		{"budget boundary", "longer than limit", nil, 4, 4, metadatamodel.ErrAssetReadLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := roundTripFunc(func(*http.Request) (*http.Response, error) {
				value := response(http.StatusOK, "image/png", test.body)
				if test.failure != nil {
					value.Body = assetReadFailure{test.failure}
				}
				return value, nil
			})
			data, err := New(client, resolverFunc(publicAssetResolver), assetTestNow).FetchAssetBounded(t.Context(), assetTestRef, test.limit)
			if !errors.Is(err, test.cause) || data.ReceivedBytes != test.bytes || len(data.Bytes) != 0 {
				t.Fatalf("bounded result bytes=%d ready=%d error=%v", data.ReceivedBytes, len(data.Bytes), err)
			}
		})
	}
}
