package emulationstationimport

import (
	"context"

	"retrom/internal/capability/format/emulationstationmeta"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

const maxGamelistBytes = int64(emulationstationmeta.MaxGameListBytes)

type (
	discoveredFile = emulationstationimportservice.DiscoveredFile
	scannedItem    = emulationstationimportmodel.ScanItem
	scanResult     emulationstationimportmodel.ScanProjection
)

func (service *Service) scan(
	ctx context.Context,
	root Root,
	selectedPath string,
	releaseYearMax int,
) (scanResult, error) {
	result, err := emulationstationimportservice.NewScanner(scannerSource{root: root, selectedPath: selectedPath}).Scan(ctx, releaseYearMax)
	return scanResult(result), err
}
