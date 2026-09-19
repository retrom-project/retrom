package hasheous

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"retrom/internal/capability/format/imagecontent"

	metadatamodel "retrom/internal/model/metadata"
)

func readAssetResponse(ctx context.Context, response *http.Response, limit int64) (metadatamodel.AssetData, error) {
	contents, readErr := io.ReadAll(io.LimitReader(assetContextReader{ctx, response.Body}, limit))
	closed := response.Body.Close()
	received := int64(len(contents))
	if err := errors.Join(readErr, closed, context.Cause(ctx)); err != nil {
		return metadatamodel.AssetData{ReceivedBytes: received}, errors.Join(metadatamodel.ErrAssetNetwork, err)
	}
	if received == limit {
		code := metadatamodel.ErrAssetReadLimit
		if limit == metadatamodel.MaximumAssetReadBytes {
			code = metadatamodel.ErrAssetTooLarge
		}
		return metadatamodel.AssetData{ReceivedBytes: received}, code
	}
	data, err := imagecontent.Validate(contents, response.Header.Get("Content-Type"))
	data.ReceivedBytes = received
	if err != nil {
		return data, fmt.Errorf("validate fetched image: %w", err)
	}
	return data, nil
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
