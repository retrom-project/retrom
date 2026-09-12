package pegasusimport

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/serversource"
	application "retrom/internal/service/pegasusimport"
)

type (
	scanResult      = application.ScanResult
	scannedMetadata = application.ScanMetadata
	scannedItem     = application.ScanItem
)

func (service *Service) executeScan(ctx context.Context, unit work, root Root) {
	result, err := service.scan(ctx, root, unit.RelativePath)
	if err != nil {
		service.fail(ctx, unit, errorCode(err), errors.Is(err, serversource.ErrRootUnavailable))
		return
	}
	if err := service.persistScan(ctx, unit, result); err != nil {
		service.fail(ctx, unit, "INTERNAL_ERROR", true)
	}
}

func (service *Service) scan(ctx context.Context, root Root, selectedPath string) (scanResult, error) {
	source := scanSource{root: root, selectedPath: selectedPath, acquire: service.acquireSourceReader}
	result, err := application.NewScanner(source).Scan(ctx)
	if err != nil {
		return scanResult{}, fmt.Errorf("scan Pegasus source: %w", err)
	}
	return result, nil
}
