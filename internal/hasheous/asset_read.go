package hasheous

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func readAssetResponse(ctx context.Context, response *http.Response, limit int64) (AssetData, error) {
	contents, readErr := io.ReadAll(io.LimitReader(assetContextReader{ctx, response.Body}, limit))
	closed := response.Body.Close()
	received := int64(len(contents))
	if err := errors.Join(readErr, closed, context.Cause(ctx)); err != nil {
		return AssetData{ReceivedBytes: received}, errors.Join(ErrAssetNetwork, err)
	}
	if received == limit {
		code := ErrAssetReadLimit
		if limit == MaximumAssetReadBytes {
			code = ErrAssetTooLarge
		}
		return AssetData{ReceivedBytes: received}, code
	}
	data, err := validateImage(contents, response.Header.Get("Content-Type"))
	data.ReceivedBytes = received
	return data, err
}

type assetContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader assetContextReader) Read(buffer []byte) (int, error) {
	if err := context.Cause(reader.ctx); err != nil {
		return 0, fmt.Errorf("read cancelled asset body: %w", err)
	}
	count, err := reader.reader.Read(buffer[:min(len(buffer), 1<<20)])
	if err != nil && err != io.EOF {
		return count, fmt.Errorf("read asset body: %w", err)
	}
	if err == io.EOF {
		return count, io.EOF
	}
	return count, nil
}
